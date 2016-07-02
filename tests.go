package main;

import (
    "time"
    "log"
    )

type bla struct { id int64; i int64 }
type int64Slice []*bla
func (s int64Slice) Len() int { return len(s) }
func (s int64Slice) Less(i, j int) bool { return s[i].id < s[j].id }
func (s int64Slice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func cmdTest() error {
    store, err := NewStore(dbFile, NewPrefixedLogger("store"))
    if err != nil {
        return err
    }
    defer store.Close()

    store.FeedReqsRemoveAll()
    return nil
}

func cmdTestFeeds() error {
    var err error

    store, err := NewStore(dbFile, NewPrefixedLogger("store"))
    if err != nil {
        return err
    }
    defer store.Close()

    feedd := NewFeedD(store, NewPrefixedLogger("feedd"))
    feedd.Start()
    defer feedd.Stop()

    time.Sleep(time.Minute * 10)

    _ = log.New

    return nil
}


