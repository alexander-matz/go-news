package feeds;

import (
    "github.com/SlyMarbo/rss"
    "time"
    "log"
    "../store"
    "errors"
    )

var daemonStop = make(chan bool, 5)
var daemonStarted = false

func daemon(stop chan bool) {
    time.Sleep(time.Second * 2)
    //lastSize := 0
    for true {
        if feeds, err := store.FeedsUrl(); err == nil {
            for _, feed := range(feeds) {
                f := feed
                go fetch(f)
            }
        } else {
            log.Printf("[feeds.rssDaemon] %s", err.Error())
        }
        select{
        case <-stop:
            return;
        case <-time.After(time.Minute * 5):
            break;
        }
    }
}

func fetch(f *store.Feed) {
    t1 := time.Now()
    if feed, err := rss.Fetch(f.Url); err == nil {
        t2 := time.Now()
        if !f.Initialized {
            fu := &store.Feed{f.Id, true, f.Handle, feed.Title, feed.Link, f.Url, feed.Image.Url}
            store.FeedsUpdate(fu)
        }
        t3 := time.Now()
        posts := make([]*store.Post, 0)
        for _, post := range(feed.Items) {
            p := &store.Post{0, post.Title, post.ID, post.Link, f.Id, post.Date}
            posts = append(posts, p)
        }
        t4 := time.Now()
        store.PostsAddBatch(posts)
        t5 := time.Now()
        log.Printf("[%s] Updated %s (%s, %s, %s, %s)", time.Now().Format("15:04:05"), f.Handle,
            t2.Sub(t1), t3.Sub(t2), t4.Sub(t3), t5.Sub(t4))
    } else {
        log.Printf("[feeds.rssFetch] %s", err.Error())
    }
}

func Init() error {
    if daemonStarted {
        return errors.New("RSS service already started")
    }
    go daemon(daemonStop)
    daemonStarted = true
    return nil
}

func Deinit() error {
    daemonStop <- true
    daemonStarted = false
    return nil
}
