package main;

import (
    "github.com/SlyMarbo/rss"
    "time"
    "log"
    )

type FeedD struct {
    stop    chan bool
    active  bool
    store   *Store
}

func NewFeedD(store *Store) *FeedD{
    res := &FeedD{make(chan bool, 1), false, store}
    return res
}

func (f *FeedD) MaxFeeds() int {
    return MaxIdGen - 256
}

func (f *FeedD) Start() {
    if !f.active {
        f.active = true
        go f.run()
    }
}

func (f *FeedD) Stop() {
    if f.active {
        f.stop <- true
        f.active = false
    }
}

func (f *FeedD) run() {
    select {
    case <-f.stop:
        return;
    case <-time.After(time.Second * 2):
        break;
    }
    for true {
        delay := time.After(time.Minute * 5)
        feeds := f.store.FeedsAll();
        if len(feeds) > f.MaxFeeds() {
            log.Fatal("[FeedD.run:%s] TOO MANY FEEDS")
        }
        for i, feed := range(feeds) {
            idgen := NewIdGen(256 + i)
            go func(feed *Feed, ids *IdGen) {
                f.fetch(feed, ids)
            }(feed, idgen)
        }
        select{
        case <-f.stop:
            return;
        case <-delay:
            break;
        }
    }
}

func (f *FeedD) fetch(ref *Feed, ids *IdGen) {
    t1 := time.Now()
    if feed, err := rss.Fetch(ref.Url); err == nil {
        if !ref.Initialized {
            newFeed := &Feed{ref.Id, true, ref.Handle, feed.Title, feed.Link, ref.Url, feed.Image.Url}
            f.store.FeedsUpdate(newFeed)
        }
        posts := make([]*Post, len(feed.Items))
        for i, post := range(feed.Items) {
            p := &Post{ids.MakeIdFromTimestamp(post.Date), post.Title, post.ID, post.Link, ref.Id, post.Date}
            posts[i] = p
        }
        f.store.PostsInsertOrIgnore(posts)
        t5 := time.Now()
        log.Printf("[FeedD.fetch:%s] Updated %s (%s)", nowString(), ref.Handle, t5.Sub(t1))
    } else {
        log.Printf("[FeedD.fetch:%s] %s", nowString(), err.Error())
    }
}
