package main

import (
	"errors"
	"log"
	"time"

	"github.com/alexander-matz/go-news/db"
	"github.com/SlyMarbo/rss"
)

type FeedD struct {
	stop   chan bool
	active bool
	store  *Store
	log    *log.Logger
	seen   map[string]bool
}

func NewFeedD(store *Store, log *log.Logger) *FeedD {
	res := &FeedD{make(chan bool), false, store, log, nil}
	return res
}

func (f *FeedD) MaxFeeds() int {
	return MaxIDGen - 256
}

func (f *FeedD) Start() error {
	if f.active {
		return errors.New("already running")
	}

	var err error
	f.seen, err = f.store.PostsGUIDMap()
	if err != nil {
		return err
	}
	f.active = true
	go f.run()
	return nil
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
		return
	case <-time.After(time.Second * 2):
		break
	}
	for true {
		delay := time.After(time.Minute * 5)

		f.log.Printf("updating")
		feeds := f.store.FeedsAll()
		if len(feeds) > f.MaxFeeds() {
			f.log.Printf("WARNING: too many feeds, ignoring some")
			feeds = feeds[:f.MaxFeeds()]
		}
		posts := make(chan *Post)
		for i, feed := range feeds {
			idgen := NewIDGen(256 + i)
			go f.fetch(feed, idgen, posts)
		}
		newposts := make([]*Post, 0)
		newmap := make(map[string]bool)
		_ = newposts
		_ = newmap
		remain := len(feeds)
		numnew := 0
		for remain > 0 {
			post := <-posts
			if post == nil {
				remain -= 1
				continue
			}
			newposts = append(newposts, post)
			newmap[post.GUID] = true
			numnew += 1
		}
		close(posts)
		f.seen = newmap
		f.store.PostsInsert(newposts)
		f.log.Printf("%d new posts", numnew)
		select {
		case <-f.stop:
			return
		case <-delay:
			break
		}
	}
}

func (f *FeedD) fetch(ref *Feed, ids *IDGen, pc chan *Post) {
	defer func() {
		pc <- nil
	}()
	maxAge := f.store.PostsMaxAge()

	feed, err := rss.Fetch(ref.URL)
	if err != nil {
		f.log.Printf("ERROR: feed %s: %s", ref.Handle, err.Error())
		return
	}
	if !ref.Initialized {
		newFeed := &Feed{ref.ID, true, ref.Handle, feed.Title, feed.Link, ref.URL, feed.Image.Url}
		f.store.FeedsSet(newFeed)
	}
	feedID := ref.ID
	for _, post := range feed.Items {
		guid := post.Link
		link := post.Link
		date := post.Date
		if date.IsZero() {
			date = time.Now()
		}
		id := ids.MakeIDFromTimestamp(date)
		title := post.Title

		if f.seen[guid] {
			continue
		}

		if date.Before(maxAge) {
			continue
		}

		var p Post
		p.ID = id
		p.Title = title
		p.GUID = guid
		p.Link = link
		p.Feed = feedID
		p.Date = date
		pc <- &p
	}
}

func updateFeeds(db *db.DB, feeds []*db.Feed) error {
	return nil
}
