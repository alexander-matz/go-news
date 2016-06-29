package main;

import (
    "sync"
    "time"
    "errors"
    "sort"
    "encoding/json"
    "encoding/binary"
    "bytes"
    "net/http"
    "io/ioutil"

    "log"

    "github.com/boltdb/bolt"

    "github.com/alexander-matz/go-news/readability"
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

type Readability struct {
    ID      int64
    URL     string
    Title   string
    Content string
}

type postByDate []*Post
func (p postByDate) Len() int { return len(p) }
// Sorting by date via id comparison only works because we're using
// twitter snowflake ids
func (p postByDate) Less(i, j int) bool { return p[i].ID > p[j].ID }
func (p postByDate) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

type readabilityByDate []*Readability
func (a readabilityByDate) Len() int { return len(a) }
func (a readabilityByDate) Less(i, j int) bool { return a[i].ID > a[j].ID }
func (a readabilityByDate) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

type Store struct {
    feeds       []*Feed
    feedMap     map[int64]*Feed

    posts       []*Post
    postMap     map[int64]*Post
    guidMap     map[string]*Post

    readMap     map[string]*Readability

    flock       sync.Mutex
    plock       sync.Mutex
    alock       sync.Mutex

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
        _, err = tx.CreateBucketIfNotExists([]byte("posts"))
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
    s.feedMap = make(map[int64]*Feed)

    s.posts = make([]*Post, 0)
    s.postMap = make(map[int64]*Post)
    s.guidMap = make(map[string]*Post)

    s.readMap = make(map[string]*Readability)

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

/******************************************************************************
 * FEEDS
 *****************************************************************************/

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

/******************************************************************************
 * POSTS
 *****************************************************************************/

func (s *Store) postCacheInvalidate() {
    s.posts = nil
    s.guidMap = nil
    s.postMap = nil
}

func (s *Store) postCacheTouch() {
    if s.posts != nil {
        return
    }
    s.posts = make([]*Post, 0)
    s.postMap = make(map[int64]*Post)
    s.guidMap = make(map[string]*Post)
    n := 0
    nerr := 0
    s.db.View(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("posts"))
        c := b.Cursor()
        for k, v := c.Last(); k != nil; k, v = c.Prev() {
            //id := int64(binary.LittleEndian.Uint64(k))
            var post Post
            err := json.Unmarshal(v, &post)
            if err != nil {
                nerr += 1
                continue
            }
            s.posts = append(s.posts, &post)
            s.postMap[post.ID] = &post
            s.guidMap[post.GUID] = &post
            n += 1
        }
        return nil
    })
}

func (s *Store) PostsInsertOrIgnore(posts []*Post) error {
    if len(posts) == 0 {
        return nil
    }
    s.plock.Lock()
    defer s.plock.Unlock()

    s.postCacheTouch()

    newPosts := make([]*Post, 0)
    for _, p := range(posts) {
        _, ok := s.guidMap[p.GUID];
        if ok {
            continue
        }
        _, ok = s.postMap[p.ID];
        if ok {
            continue
        }
        newPosts = append(newPosts, p)
    }

    s.db.Update(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("posts"))
        for _, p := range(newPosts) {
            var k [8]byte
            binary.LittleEndian.PutUint64(k[:], uint64(p.ID))
            v, err := json.Marshal(p)
            if err == nil {
                b.Put(k[:], v)
            }
        }
        return nil
    })
    s.postCacheInvalidate()
    return nil
}

func (s *Store) PostsAll(n int) []*Post {
    s.plock.Lock()
    defer s.plock.Unlock()
    s.postCacheTouch()

    if n == -1 { n = len(s.posts) }
    if n > len(s.posts) { n = len(s.posts) }
    res := make([]*Post, n)
    for i := 0; i < n; i += 1 {
        res[i] = &(*s.posts[i])
    }
    return res
}

func (s *Store) PostsAllAfter(n int, after time.Time) []*Post {
    s.plock.Lock()
    defer s.plock.Unlock()
    s.postCacheTouch()

    if n == -1 { n = len(s.posts) }
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
    s.postCacheTouch()

    if n == -1 { n = len(s.posts) }
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

func (s *Store) PostsByFeedsAfter(n int, feeds []string, after time.Time) []*Post {
    s.plock.Lock()
    defer s.plock.Unlock()
    s.postCacheTouch()

    if n == -1 { n = len(s.posts) }
    res := make([]*Post, 0)
    i := sort.Search(len(s.posts), func(i int) bool { return after.Before(s.posts[i].Date) })
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
    s.plock.Lock()
    defer s.plock.Unlock()
    s.postCacheTouch()

    p, ok := s.postMap[id]
    if !ok { return nil; }
    return &(*p)
}

func (s *Store) PostsTrimByTime(until time.Time) error {
    s.plock.Lock()
    defer s.plock.Unlock()

    err := s.db.Update(func (tx *bolt.Tx) error {
        t := until.UnixNano() / (int64(time.Millisecond)/int64(time.Nanosecond))
        b := tx.Bucket([]byte("posts"))
        c := b.Cursor()
        var start [8]byte
        binary.LittleEndian.PutUint64(start[:], uint64(t))
        for k, _ := c.Seek(start[:]); k != nil && bytes.Compare(k, start[:]) <= 0; k, _ = c.Next() {
            err := b.Delete(k)
            if err != nil {
                log.Printf("WARNING: UNABLE TO TRIM DATABASE ELEMENT")
            }
        }
        return nil
    })
    s.postCacheInvalidate()
    return err
}

func (s *Store) PostsTrimByNumber(n int) error {
    s.plock.Lock()
    defer s.plock.Unlock()

    err := s.db.Update(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("posts"))
        c := b.Cursor()
        for k, _ := c.First(); k != nil; k, _ = c.Next() {
            n -= 1;
            if n == 0 {
                b.Delete(k)
            }
        }
        return nil
    })
    s.postCacheInvalidate()
    return err
}

/******************************************************************************
 * READABILITY
 *****************************************************************************/

func (s *Store) readabilityTrim(n int) {
    s.alock.Lock()
    defer s.alock.Unlock()

    if len(s.readMap) < n {
        return
    }

    list := make([]*Readability, len(s.readMap))
    i := 0
    for _, r := range(s.readMap) {
        list[i] = r
        i += 1
    }
    sort.Sort(readabilityByDate(list))
    for i = n; i < len(list); i += 1 {
        delete(s.readMap, list[i].URL)
    }
}

func (s *Store) fetchReadability(url string) (*Readability, error) {
    res, err := http.Get(url)
    if err != nil { return nil, err }
    defer res.Body.Close()
    html, err := ioutil.ReadAll(res.Body)
    if err != nil { return nil, err }
    doc, err := readability.NewDocument(string(html))
    if err != nil { return nil, err }
    r := &Readability{MakeID(), url, "", doc.Content()}
    s.readMap[url] = r
    s.readabilityTrim(512)

    return r, nil
}

func (s *Store) ReadabilityGetOne(id int64) (*Readability, error) {
    s.plock.Lock()
    s.postCacheTouch()

    p, ok := s.postMap[id]
    if !ok {
        s.plock.Unlock()
        return nil, errors.New("invalid article id")
    }
    s.plock.Unlock()

    s.alock.Lock()
    r, ok := s.readMap[p.Link]
    s.alock.Unlock()

    if ok {
        return &(*r), nil
    }
    r, err := s.fetchReadability(p.Link)
    if err != nil {
        return nil, err
    }

    return r, nil
}
