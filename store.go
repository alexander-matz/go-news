package main;

import (
    "sync"
    "time"
    "errors"
    "sort"
    "strings"
    "encoding/json"
    "encoding/binary"
    _ "bytes"
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
    ID      int64       `json:"id"`
    Title   string      `json:"title"`
    GUID    string      `json:"guid"`
    Link    string      `json:"link"`
    Feed    int64       `json:"feed"`
    Date    time.Time   `json:"date"`
}

type Readability struct {
    ID      int64
    URL     string
    Title   string
    Content string
}

type feedByHandle []*Feed
func (p feedByHandle) Len() int { return len(p) }
func (p feedByHandle) Less(i, j int) bool { return strings.Compare(p[i].Handle, p[j].Handle) > 0 }
func (p feedByHandle) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

type postByDate []*Post
func (p postByDate) Len() int { return len(p) }
// Sorting by date via id comparison only works because we're using
// twitter snowflake ids
func (p postByDate) Less(i, j int) bool { return p[i].Date.After(p[j].Date) }
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

    readMap     map[string]*Readability

    flock       sync.Mutex
    plock       sync.Mutex
    alock       sync.Mutex

    db          *bolt.DB

    postsHold   time.Duration
    readHold    int
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
        _, err = tx.CreateBucketIfNotExists([]byte("guids"))
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

    s.readMap = make(map[string]*Readability)

    s.db = db

    s.postsHold = time.Hour * 24 * 2
    s.readHold  = 64

    return &s, nil
}

func (s *Store) Dump() {
    s.db.View(func (tx *bolt.Tx) error {
        log.Printf("Bucket 'feeds':")
        b := tx.Bucket([]byte("feeds"))
        c := b.Cursor()
        for k, v := c.First(); k != nil; k, v = c.Next() {
            var feed Feed
            id := int64(binary.BigEndian.Uint64(k))
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

func (s *Store) feedsCacheTouch() {
    if s.feeds != nil { return }

    s.feeds = make([]*Feed, 0)
    s.feedMap = make(map[int64]*Feed)

    _ = s.db.View(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("feeds"))
        c := b.Cursor()
        for k, v := c.First(); k != nil; k, v = c.Next() {
            var feed Feed
            err := json.Unmarshal(v, &feed)
            if err != nil {
                log.Printf("[store:%s] ERROR UNMARSHALING FEED", nowString())
                continue
            }
            s.feeds = append(s.feeds, &feed)
            s.feedMap[feed.ID] = &feed
        }
        return nil
    })
    sort.Sort(feedByHandle(s.feeds))
}

func (s *Store) feedsCacheInvalidate() {
    s.feeds = nil
    s.feedMap = nil
}

func (s *Store) FeedsSet(f *Feed) error {
    if f.ID <= 0 { return errors.New("invalid feed id") }
    if f.URL == "" { return errors.New("invalid feed url") }
    if f.Handle == "" { return errors.New("invalid feed handle") }

    s.flock.Lock()
    defer s.flock.Unlock()
    s.feedsCacheTouch()

    for _, feed := range(s.feeds) {
        if feed.ID == f.ID {
            continue
        }
        if feed.URL == f.URL { return errors.New("feed url already exists") }
        if feed.Handle == f.Handle { return errors.New("feed handle already exists") }
    }

    // add feeds to database
    err := s.db.Update(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("feeds"))
        v, err := json.Marshal(f)
        if err != nil {
            return err
        }
        var k [8]byte
        binary.BigEndian.PutUint64(k[:], uint64(f.ID))
        b.Put(k[:], v)
        return nil
    })
    if err != nil {
        return err
    }

    s.feedsCacheInvalidate()
    return nil
}

func (s *Store) FeedsAll() []*Feed {
    s.flock.Lock()
    defer s.flock.Unlock()
    s.feedsCacheTouch()

    return s.feeds
}

func (s *Store) FeedsAllMap() map[int64]*Feed {
    s.flock.Lock()
    defer s.flock.Unlock()
    s.feedsCacheTouch()

    return s.feedMap
}

/******************************************************************************
 * POSTS
 *****************************************************************************/

func (s *Store) postCacheInvalidate() {
    s.posts = nil
    s.postMap = nil
}

func (s *Store) postCacheTouch() {
    if s.posts != nil {
        return
    }
    s.posts = make([]*Post, 0)
    s.postMap = make(map[int64]*Post)
    s.db.View(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("posts"))
        c := b.Cursor()
        for k, v := c.Last(); k != nil; k, v = c.Prev() {
            var post Post
            err := json.Unmarshal(v, &post)
            if err != nil {
                continue
            }
            s.posts = append(s.posts, &post)
            s.postMap[post.ID] = &post
        }
        return nil
    })
}

func (s *Store) PostsInsertOrIgnore(posts []*Post) error {
    if len(posts) == 0 {
        return nil
    }

    maxAge := time.Now().Add(s.postsHold * -1)

    s.db.Update(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("posts"))
        guids := tx.Bucket([]byte("guids"))
        for _, p := range(posts) {
            if p.Date.Before(maxAge) {
                continue
            }
            if guids.Get([]byte(p.GUID)) != nil {
                continue
            }
            v, err := json.Marshal(p)
            if err != nil {
                continue
            }
            var k [8]byte
            binary.BigEndian.PutUint64(k[:], uint64(p.ID))
            b.Put(k[:], v)
            guids.Put([]byte(p.GUID), []byte(""))
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

    return s.posts[:n]
}

func (s *Store) PostsAllAfter(n int, after time.Time) []*Post {
    s.plock.Lock()
    defer s.plock.Unlock()
    s.postCacheTouch()

    if n == -1 { n = len(s.posts) }
    start := sort.Search(len(s.posts), func(i int) bool { return after.After(s.posts[i].Date) })
    if start > len(s.posts) { return make([]*Post, 0) }
    end := start + n
    if end > len(s.posts) { end = len(s.posts) }
    return s.posts[start:end]
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

    s.flock.Lock()
    defer s.flock.Unlock()
    s.feedsCacheTouch()


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

    s.flock.Lock()
    defer s.flock.Unlock()
    s.feedsCacheTouch()

    if n == -1 { n = len(s.posts) }
    res := make([]*Post, 0)
    i := sort.Search(len(s.posts), func(i int) bool { return after.After(s.posts[i].Date) })
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

func (s *Store) PostsTrim() {
    n := 0
    _ = s.db.Update(func (tx *bolt.Tx) error {
        t := MakeIDRaw(time.Now().Add(s.postsHold * -1), 0, 0)
        b := tx.Bucket([]byte("posts"))
        guids := tx.Bucket([]byte("guids"))
        c := b.Cursor()
        var start [8]byte
        binary.BigEndian.PutUint64(start[:], uint64(t))
        for k, v := c.Seek(start[:]); k != nil; k, v = c.Prev() {
            err := b.Delete(k)
            if err != nil {
                log.Printf("WARNING: UNABLE TO TRIM DATABASE ELEMENT")
                continue
            }
            var post Post
            err = json.Unmarshal(v, &post)
            if err != nil {
                log.Printf("WARNING: UNABLE TO TRIM DATABASE ELEMENT")
                continue
            }
            err = guids.Delete([]byte(post.GUID))
            if err != nil {
                log.Printf("WARNING: UNABLE TO TRIM DATABASE ELEMENT")
                continue
            }
            n += 1
        }
        return nil
    })
    log.Printf("[store:%s] Trimmed %d posts", nowString(), n)
    s.postCacheInvalidate()
}

/******************************************************************************
 * READABILITY
 *****************************************************************************/

func (s *Store) readabilityTrim() {
    s.alock.Lock()
    defer s.alock.Unlock()

    n := s.readHold

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
    s.readabilityTrim()

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
