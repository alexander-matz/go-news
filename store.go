package main;

import (
    "sync"
    "time"
    "errors"
    "sort"
    "encoding/json"
    "encoding/binary"

    "log"

    "github.com/boltdb/bolt"
    )

type Feed struct {
    ID          int64   `json:"id"`
    Initialized bool    `json:"initialized"`
    Handle      string  `json:"handle"`
    Title       string  `json:"title"`
    Link        string  `json:"link"`
    URL         string  `json:"url"`
    ImageURL    string  `json:"imageurl"`
}

type Post struct {
    ID      int64
    Title   string
    GUID    string
    Link    string
    Feed    int64
    Date    time.Time
}
type postByDate []*Post
func (p postByDate) Len() int { return len(p) }
// Sorting by date via id comparison only works because we're using
// twitter snowflake ids
func (p postByDate) Less(i, j int) bool { return p[i].ID < p[j].ID }
func (p postByDate) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

type Store struct {
    feeds       []*Feed
    feedMap     map[int64]*Feed

    posts       []*Post
    postMap     map[int64]*Post
    guidMap     map[string]*Post

    flock       sync.Mutex
    plock       sync.Mutex

    db          *bolt.DB
}

func NewStore(file string) (*Store, error) {
    db, err := bolt.Open(file, 0600, &bolt.Options{Timeout: 5 * time.Second})
    if err != nil {
        return nil, err
    }
    err = db.Update(func (tx *bolt.Tx) error {
        _, err := tx.CreateBucketIfNotExists([]byte("feeds"))
        if err != nil {
            return err
        }
        return nil
    })
    if err != nil {
        db.Close()
        return nil, err
    }

    var s Store
    s.feeds = make([]*Feed, 0)
    s.posts = make([]*Post, 0)
    s.feedMap = make(map[int64]*Feed)
    s.postMap = make(map[int64]*Post)
    s.guidMap = make(map[string]*Post)

    s.db = db

    // read feeds
    err = db.View(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("feeds"))
        c := b.Cursor()
        for k, v := c.First(); k != nil; k, v = c.Next() {
            var feed Feed
            err := json.Unmarshal(v, &feed)
            if err != nil {
                return err
            }
            s.feeds = append(s.feeds, &feed)
            s.feedMap[feed.ID] = &feed
        }
        return nil
    })
    if err != nil {
        db.Close()
        return nil, err
    }

    return &s, nil
}

func (s *Store) Dump() {
    s.db.View(func (tx *bolt.Tx) error {
        log.Printf("Bucket 'feeds':")
        b := tx.Bucket([]byte("feeds"))
        c := b.Cursor()
        for k, v := c.First(); k != nil; k, v = c.Next() {
            var feed Feed
            id := int64(binary.LittleEndian.Uint64(k))
            err := json.Unmarshal(v, &feed)
            if err == nil {
                log.Printf("  %d = %s", id, feed)
            } else {
                log.Printf("  %d = INVALID JSON", id)
            }
        }
        return nil
    })
}

func (s *Store) Close() {
    s.db.Close()
    return
}

func (s *Store) FeedsSet(f *Feed) error {
    if f.ID <= 0 { return errors.New("invalid feed id") }
    if f.URL == "" { return errors.New("invalid feed url") }
    if f.Handle == "" { return errors.New("invalid feed handle") }

    s.flock.Lock()
    defer s.flock.Unlock()

    for _, feed := range(s.feeds) {
        if feed.ID == f.ID {
            continue
        }
        if feed.URL == f.URL { return errors.New("feed url already exists") }
        if feed.Handle == f.Handle { return errors.New("feed handle already exists") }
    }
    newFeed := &(*f)

    // add feeds to database
    err := s.db.Update(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("feeds"))
        v, err := json.Marshal(f)
        if err != nil {
            return err
        }
        var k [8]byte
        binary.LittleEndian.PutUint64(k[:], uint64(f.ID))
        b.Put(k[:], v)
        return nil
    })
    if err != nil {
        return err
    }

    s.feeds = append(s.feeds, newFeed)
    s.feedMap[newFeed.ID] = newFeed
    return nil
}

func (s *Store) FeedsAll() []*Feed {
    res := make([]*Feed, len(s.feeds))
    for i, v := range(s.feeds) {
        res[i] = &(*v)
    }
    return res
}

func (s *Store) FeedsAllMap() map[int64]*Feed {
    res := make(map[int64]*Feed)
    for _, v := range(s.feeds) {
        res[v.ID] = v
    }
    return res
}

func (s *Store) PostsInsertOrIgnore(posts []*Post) error {
    s.plock.Lock()
    defer s.plock.Unlock()
    for _, p := range(posts) {
        _, ok := s.guidMap[p.GUID];
        if ok {
            continue
        }
        _, ok = s.postMap[p.ID];
        if ok {
            continue
        }
        newPost := &Post{p.ID, p.Title, p.GUID, p.Link, p.Feed, p.Date}
        s.posts = append(s.posts, newPost)
        s.guidMap[newPost.GUID] = newPost
        s.postMap[newPost.ID] = newPost
    }
    sort.Sort(postByDate(s.posts))
    return nil
}

func (s *Store) PostsAll(n int) []*Post {
    s.plock.Lock()
    defer s.plock.Unlock()
    if n > len(s.posts) { n = len(s.posts) }
    res := make([]*Post, n)
    for i := 0; i < n; i += 1 {
        res[i] = &(*s.posts[i])
    }
    return res
}

func (s *Store) PostsAllAfter(after time.Time, n int) []*Post {
    s.plock.Lock()
    defer s.plock.Unlock()
    start := sort.Search(len(s.posts), func(i int) bool { return after.Before(s.posts[i].Date) })
    end := start + n
    if end > len(s.posts) { end = len(s.posts) }
    res := make([]*Post, end-start)
    pos := 0
    for i := start; i < end; i += 1 {
        res[pos] = &(*s.posts[i])
    }
    return res
}

func stringInSlice(a string, list []string) bool {
    for _, b := range list {
        if b == a {
            return true
        }
    }
    return false
}

func (s *Store) PostsByFeeds(n int, feeds []string) []*Post {
    s.plock.Lock()
    defer s.plock.Unlock()
    res := make([]*Post, 0)
    i := 0
    for n > 0 && i < len(s.posts) {
        post := s.posts[i]
        feedhandle := s.feedMap[post.Feed].Handle
        if stringInSlice(feedhandle, feeds) {
            res = append(res, &(*post))
            n -= 1
        }
        i += 1
    }
    return res
}

func (s *Store) PostsID(id int64) *Post {
    p, ok := s.postMap[id]
    if !ok { return nil; }
    return &(*p)
}

func (s *Store) TrimByTime(until time.Time) {
    s.plock.Lock()
    defer s.plock.Unlock()

    s.plock.Lock()
    defer s.plock.Unlock()
    cutPoint := sort.Search(len(s.posts), func(i int) bool { return until.Before(s.posts[i].Date) })
    for i := cutPoint; i < len(s.posts); i += 1 {
        delete(s.guidMap, s.posts[i].GUID)
        delete(s.postMap, s.posts[i].ID)
        s.posts[i] = nil
    }
    s.posts = s.posts[:cutPoint]
}

func (s *Store) TrimByNumber(n int) {
    s.plock.Lock()
    defer s.plock.Unlock()
    if (n > len(s.posts)) { return }
    for i := n; i < len(s.posts); i += 1 {
        delete(s.guidMap, s.posts[i].GUID)
        delete(s.postMap, s.posts[i].ID)
        s.posts[i] = nil
    }
    s.posts = s.posts[:n]
}
