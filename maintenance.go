package main;

import (
    "log"
    "os"
    _ "bytes"
    )

func cmdInit() error {
    var err error
    store, err := NewStore(dbFile, log.New(os.Stderr, "LOG|", 0))
    if err != nil { return err }
    defer store.Close()
    store.Init()
    return nil
}


func cmdInitDefaults() error {
    var err error

    os.Remove(dbFile)
    store, err := NewStore(dbFile, NewPrefixedLogger("store"))
    if err != nil {
        return err
    }
    defer store.Close()
    err = store.Init()
    if err != nil {
        return err
    }

    var feed Feed

    logger.Printf("Adding feeds")
    feed.ID = MakeID()
    feed.URL = "http://feeds.bbci.co.uk/news/rss.xml"
    feed.Handle = "bbc"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://feeds.bbci.co.uk/news/world/europe/rss.xml"
    feed.Handle = "bbce"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "https://en.wikinews.org/w/index.php?title=Special:NewsFeed&feed=atom&categories=Published&notcategories=No%20publish%7CArchived%7CAutoArchived%7Cdisputed&namespace=0&count=30&hourcount=124&ordermethod=categoryadd&stablepages=only"
    feed.Handle = "wik"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://rss.csmonitor.com/feeds/csm"
    feed.Handle = "csm"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://www.aljazeera.com/xml/rss/all.xml"
    feed.Handle = "alj"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://www.economist.com/sections/culture/rss.xml"
    feed.Handle = "ecoc"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://www.economist.com/sections/international/rss.xml"
    feed.Handle = "ecoi"
    if err = store.FeedsSet(&feed); err != nil { return err }

    return nil
}
