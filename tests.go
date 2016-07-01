package main;

import (
    "time"
    )

type bla struct { id int64; i int64 }
type int64Slice []*bla
func (s int64Slice) Len() int { return len(s) }
func (s int64Slice) Less(i, j int) bool { return s[i].id < s[j].id }
func (s int64Slice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func cmdTest() error {
    var in time.Time
    var out time.Time
    in = time.Now()
    out = TimeFromID(MakeIDRaw(in, 0xffffffffff, 0xffffffffffff))
    logger.Printf("%s --- %s", in, out)
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

    return nil
}


