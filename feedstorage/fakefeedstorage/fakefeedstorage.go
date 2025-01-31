package fakefeedstorage

import (
	"context"
	"github.com/webhookdb/icalproxy/feedstorage"
	"sync"
)

type FakeFeedStorage struct {
	Files map[int64][]byte
	mux   *sync.Mutex
}

func (f *FakeFeedStorage) Store(_ context.Context, feedId int64, body []byte) error {
	f.mux.Lock()
	defer f.mux.Unlock()
	f.Files[feedId] = body
	return nil
}

func (f *FakeFeedStorage) Fetch(_ context.Context, feedId int64) ([]byte, error) {
	f.mux.Lock()
	defer f.mux.Unlock()
	b, ok := f.Files[feedId]
	if !ok {
		return nil, feedstorage.ErrNotFound
	}
	return b, nil
}

func New() *FakeFeedStorage {
	return &FakeFeedStorage{Files: make(map[int64][]byte), mux: new(sync.Mutex)}
}

var _ feedstorage.Interface = &FakeFeedStorage{}
