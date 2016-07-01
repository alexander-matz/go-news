package main;

import (
    "log"
    "os"
    "fmt"
    "encoding/hex"
    "encoding/json"
    "errors"
    _ "bytes"
    "crypto/sha1"
    "io/ioutil"

    "golang.org/x/crypto/ssh/terminal"
    )

func cmdInit() error {
    var err error
    os.Remove(dbFile)
    store, err := NewStore(dbFile, log.New(os.Stderr, "LOG|", 0))
    if err != nil { return err }
    defer store.Close()
    return nil
}

func cmdBackup() error {
    logger := log.New(os.Stderr, "LOG| ", 0)

    optindent := false
    optfeeds := true
    optposts := true
    optfeedreqs := true
    for i := 2; i < len(os.Args); i += 1 {
        switch os.Args[i] {
        case "-indent": optindent = true
        case "-nofeeds": optfeeds = false
        case "-noposts": optposts = false
        case "-nofeedreqs": optfeedreqs = false
        default:
            return errors.New("unrecognized option: " + os.Args[i])
        }
    }

    obj := make(map[string]interface{})

    store, err := NewStore(dbFile, logger)
    if err != nil {
        return err;
    }
    defer store.Close()

    obj["dbversion"] = "1.0"

    if optfeeds {
        obj["feeds"] = store.FeedsAll()
    }

    if  optposts {
        obj["posts"] = store.PostsAll(-1)
    }
    if optfeedreqs {
        s, err := store.FeedReqsAll()
        if err != nil {
            return err
        }
        obj["feedrequests"] = s
    }

    if optindent {
        data, err := json.MarshalIndent(obj, "", "  ")
        if err != nil {
            return err
        }
        fmt.Fprintln(os.Stdout, string(data))
    } else {
        data, err := json.Marshal(obj)
        if err != nil {
            return err
        }
        fmt.Fprintln(os.Stdout, string(data))
    }
    return nil
}

func cmdRestore() error {
    logger := log.New(os.Stderr, "LOG| ", 0)

    optfeeds := true
    optposts := true
    optfeedreqs := true
    _ = optfeeds
    _ = optposts
    _ = optfeedreqs
    for i := 2; i < len(os.Args); i += 1 {
        switch os.Args[i] {
        case "-nofeeds": optfeeds = false
        case "-noposts": optposts = false
        case "-nofeedreqs": optfeedreqs = false
        default:
            return errors.New("unrecognized option: " + os.Args[i])
        }
    }

    input, err := ioutil.ReadAll(os.Stdin)
    if err != nil {
        return errors.New("unable to decode json")
    }
    obj := make(map[string]interface{})
    err = json.Unmarshal(input, &obj)
    if err != nil {
        return errors.New("unable to decode json")
    }
    logger.Fatal("Not implemented yet")
    return nil
}

func cmdHash() error {
    oldState, err := terminal.MakeRaw(0)
    if err != nil {
        return err
    }
    defer terminal.Restore(0, oldState)
    term := terminal.NewTerminal(os.Stdin, "")
    pw, err := term.ReadPassword("Enter password: ")
    if err != nil { return err }
    hash := sha1.Sum([]byte(pw))
    logger.Printf("password Hash: %s", hex.EncodeToString(hash[:]))
    return nil
}

/******************************************************************************
 * Debug stuff
 */

func cmdInitDebug() error {
    var err error

    os.Remove(dbFile)
    store, err := NewStore(dbFile, NewPrefixedLogger("store"))
    if err != nil {
        return err
    }
    defer store.Close()

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
