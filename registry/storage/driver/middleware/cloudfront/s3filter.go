package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/2DFS/2dfs-registry/v3/internal/dcontext"
	"github.com/2DFS/2dfs-registry/v3/internal/requestutil"
)

const (
	// ipRangesURL is the URL to get definition of AWS IPs
	defaultIPRangesURL = "https://ip-ranges.amazonaws.com/ip-ranges.json"
	// updateFrequency tells how frequently AWS IPs need to be updated
	defaultUpdateFrequency = time.Hour * 12
)

// newAWSIPs returns a New awsIP object.
// If awsRegion is `nil`, it accepts any region. Otherwise, it only allow the regions specified
func newAWSIPs(ctx context.Context, host string, updateFrequency time.Duration, awsRegion []string) (*awsIPs, error) {
	ips := &awsIPs{
		host:            host,
		updateFrequency: updateFrequency,
		awsRegion:       awsRegion,
		updaterStopChan: make(chan bool),
	}
	if err := ips.tryUpdate(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		dcontext.GetLogger(ctx).WithError(err).Warn("failed to update AWS IP")
	}
	go ips.updater()
	return ips, nil
}

// awsIPs tracks a list of AWS ips, filtered by awsRegion
type awsIPs struct {
	host            string
	updateFrequency time.Duration
	ipv4            []net.IPNet
	ipv6            []net.IPNet
	mutex           sync.RWMutex
	awsRegion       []string
	updaterStopChan chan bool
	initialized     bool
}

type awsIPResponse struct {
	Prefixes   []prefixEntry `json:"prefixes"`
	V6Prefixes []prefixEntry `json:"ipv6_prefixes"`
}

type prefixEntry struct {
	IPV4Prefix string `json:"ip_prefix"`
	IPV6Prefix string `json:"ipv6_prefix"`
	Region     string `json:"region"`
	Service    string `json:"service"`
}

func fetchAWSIPs(ctx context.Context, url string) (awsIPResponse, error) {
	var response awsIPResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return response, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return response, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return response, fmt.Errorf("failed to fetch network data. response = %s", body)
	}
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&response)
	if err != nil {
		return response, err
	}
	return response, nil
}

// tryUpdate attempts to download the new set of ip addresses.
// tryUpdate must be thread safe with contains
func (s *awsIPs) tryUpdate(ctx context.Context) error {
	response, err := fetchAWSIPs(ctx, s.host)
	if err != nil {
		return err
	}

	var ipv4 []net.IPNet
	var ipv6 []net.IPNet

	processAddress := func(output *[]net.IPNet, prefix string, region string) {
		regionAllowed := false
		if len(s.awsRegion) > 0 {
			for _, ar := range s.awsRegion {
				if strings.EqualFold(region, ar) {
					regionAllowed = true
					break
				}
			}
		} else {
			regionAllowed = true
		}

		_, network, err := net.ParseCIDR(prefix)
		if err != nil {
			dcontext.GetLoggerWithFields(dcontext.Background(), map[interface{}]interface{}{
				"cidr": prefix,
			}).Error("unparseable cidr")
			return
		}
		if regionAllowed {
			*output = append(*output, *network)
		}
	}

	for _, prefix := range response.Prefixes {
		processAddress(&ipv4, prefix.IPV4Prefix, prefix.Region)
	}
	for _, prefix := range response.V6Prefixes {
		processAddress(&ipv6, prefix.IPV6Prefix, prefix.Region)
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	// Update each attr of awsips atomically.
	s.ipv4 = ipv4
	s.ipv6 = ipv6
	s.initialized = true
	return nil
}

// This function is meant to be run in a background goroutine.
// It will periodically update the ips from aws.
func (s *awsIPs) updater() {
	ctx := context.TODO()
	defer close(s.updaterStopChan)
	for {
		time.Sleep(s.updateFrequency)
		select {
		case <-s.updaterStopChan:
			dcontext.GetLogger(ctx).Info("aws ip updater received stop signal")
			return
		default:
			err := s.tryUpdate(ctx)
			if err != nil {
				dcontext.GetLogger(ctx).WithError(err).Error("git  AWS IP")
			}
		}
	}
}

// getCandidateNetworks returns either the ipv4 or ipv6 networks
// that were last read from aws. The networks returned
// have the same type as the ip address provided.
func (s *awsIPs) getCandidateNetworks(ip net.IP) []net.IPNet {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if ip.To4() != nil {
		return s.ipv4
	} else if ip.To16() != nil {
		return s.ipv6
	} else {
		dcontext.GetLoggerWithFields(dcontext.Background(), map[interface{}]interface{}{
			"ip": ip,
		}).Error("unknown ip address format")
		// assume mismatch, pass through cloudfront
		return nil
	}
}

// Contains determines whether the host is within aws.
func (s *awsIPs) contains(ip net.IP) bool {
	networks := s.getCandidateNetworks(ip)
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// parseIPFromRequest attempts to extract the ip address of the
// client that made the request
func parseIPFromRequest(request *http.Request) (net.IP, error) {
	ipStr := requestutil.RemoteIP(request)
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid ip address from requester: %s", ipStr)
	}

	return ip, nil
}

// eligibleForS3 checks if a request is eligible for using S3 directly
// Return true only when the IP belongs to a specific aws region and user-agent is docker
func eligibleForS3(request *http.Request, awsIPs *awsIPs) bool {
	if awsIPs != nil && awsIPs.initialized {
		if addr, err := parseIPFromRequest(request); err == nil {
			loggerField := map[interface{}]interface{}{
				"user-client": request.UserAgent(),
				"ip":          requestutil.RemoteIP(request),
			}
			if awsIPs.contains(addr) {
				dcontext.GetLoggerWithFields(request.Context(), loggerField).Info("request from the allowed AWS region, skipping CloudFront")
				return true
			}
			dcontext.GetLoggerWithFields(request.Context(), loggerField).Warn("request not from the allowed AWS region, fallback to CloudFront")
		} else {
			dcontext.GetLogger(request.Context()).WithError(err).Warn("failed to parse ip address from context, fallback to CloudFront")
		}
	}
	return false
}
