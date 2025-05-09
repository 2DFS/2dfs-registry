package proxy

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"testing"

	distribution "github.com/2DFS/2dfs-registry/v3"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type mockTagStore struct {
	mapping map[string]v1.Descriptor
	sync.Mutex
	distribution.TagService
}

func (m *mockTagStore) Get(ctx context.Context, tag string) (v1.Descriptor, error) {
	m.Lock()
	defer m.Unlock()

	if d, ok := m.mapping[tag]; ok {
		return d, nil
	}
	return v1.Descriptor{}, distribution.ErrTagUnknown{}
}

func (m *mockTagStore) Tag(ctx context.Context, tag string, desc v1.Descriptor) error {
	m.Lock()
	defer m.Unlock()

	m.mapping[tag] = desc
	return nil
}

func (m *mockTagStore) Untag(ctx context.Context, tag string) error {
	m.Lock()
	defer m.Unlock()

	if _, ok := m.mapping[tag]; ok {
		delete(m.mapping, tag)
		return nil
	}
	return distribution.ErrTagUnknown{}
}

func (m *mockTagStore) All(ctx context.Context) ([]string, error) {
	m.Lock()
	defer m.Unlock()

	tags := make([]string, 0, len(m.mapping))
	for tag := range m.mapping {
		tags = append(tags, tag)
	}

	return tags, nil
}

func testProxyTagService(local, remote map[string]v1.Descriptor) *proxyTagService {
	if local == nil {
		local = make(map[string]v1.Descriptor)
	}
	if remote == nil {
		remote = make(map[string]v1.Descriptor)
	}
	return &proxyTagService{
		localTags:      &mockTagStore{mapping: local},
		remoteTags:     &mockTagStore{mapping: remote},
		authChallenger: &mockChallenger{},
	}
}

func TestGet(t *testing.T) {
	remoteDesc := v1.Descriptor{Size: 42}
	remoteTag := "remote"
	proxyTags := testProxyTagService(map[string]v1.Descriptor{remoteTag: remoteDesc}, nil)

	ctx := context.Background()

	// Get pre-loaded tag
	d, err := proxyTags.Get(ctx, remoteTag)
	if err != nil {
		t.Fatal(err)
	}

	if proxyTags.authChallenger.(*mockChallenger).count != 1 {
		t.Fatalf("Expected 1 auth challenge call, got %#v", proxyTags.authChallenger)
	}

	if !reflect.DeepEqual(d, remoteDesc) {
		t.Fatal("unable to get put tag")
	}

	local, err := proxyTags.localTags.Get(ctx, remoteTag)
	if err != nil {
		t.Fatal("remote tag not pulled into store")
	}

	if !reflect.DeepEqual(local, remoteDesc) {
		t.Fatal("unexpected descriptor pulled through")
	}

	// Manually overwrite remote tag
	newRemoteDesc := v1.Descriptor{Size: 43}
	err = proxyTags.remoteTags.Tag(ctx, remoteTag, newRemoteDesc)
	if err != nil {
		t.Fatal(err)
	}

	d, err = proxyTags.Get(ctx, remoteTag)
	if err != nil {
		t.Fatal(err)
	}

	if proxyTags.authChallenger.(*mockChallenger).count != 2 {
		t.Fatalf("Expected 2 auth challenge calls, got %#v", proxyTags.authChallenger)
	}

	if !reflect.DeepEqual(d, newRemoteDesc) {
		t.Fatal("unable to get put tag")
	}

	_, err = proxyTags.localTags.Get(ctx, remoteTag)
	if err != nil {
		t.Fatal("remote tag not pulled into store")
	}

	// untag, ensure it's removed locally, but present in remote
	err = proxyTags.Untag(ctx, remoteTag)
	if err != nil {
		t.Fatal(err)
	}

	_, err = proxyTags.localTags.Get(ctx, remoteTag)
	if err == nil {
		t.Fatal("Expected error getting Untag'd tag")
	}

	_, err = proxyTags.remoteTags.Get(ctx, remoteTag)
	if err != nil {
		t.Fatal("remote tag should not be untagged with proxyTag.Untag")
	}

	_, err = proxyTags.Get(ctx, remoteTag)
	if err != nil {
		t.Fatal("untagged tag should be pulled through")
	}

	if proxyTags.authChallenger.(*mockChallenger).count != 3 {
		t.Fatalf("Expected 3 auth challenge calls, got %#v", proxyTags.authChallenger)
	}

	// Add another tag.  Ensure both tags appear in 'All'
	err = proxyTags.remoteTags.Tag(ctx, "funtag", v1.Descriptor{Size: 42})
	if err != nil {
		t.Fatal(err)
	}

	all, err := proxyTags.All(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(all) != 2 {
		t.Fatalf("Unexpected tag length returned from All() : %d ", len(all))
	}

	sort.Strings(all)
	if all[0] != "funtag" && all[1] != "remote" {
		t.Fatalf("Unexpected tags returned from All() : %v ", all)
	}

	if proxyTags.authChallenger.(*mockChallenger).count != 4 {
		t.Fatalf("Expected 4 auth challenge calls, got %#v", proxyTags.authChallenger)
	}
}
