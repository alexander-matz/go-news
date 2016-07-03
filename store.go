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
    Title       string  `json:"title",omitempty`
    Link        string  `json:"link",omitempty`
    URL         string  `json:"url"`
    ImageURL    string  `json:"imageurl",omitempty`
}

type Post struct {
    ID      int64       `json:"id"`
    Title   string      `json:"title"`
    GUID    string      `json:"guid"`
    Link    string      `json:"link"`
    Feed    int64       `json:"feed"`
    Date    time.Time   `json:"-"`
}

type FeedReq struct {
    ID      int64       `json:"id"`
    URL     string      `json:"url"`
    N       int         `json:"n"`
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

type FeedReqsByCount []*FeedReq
func (a FeedReqsByCount) Len() int { return len(a) }
func (a FeedReqsByCount) Less(i, j int) bool { return a[i].N < a[j].N }
func (a FeedReqsByCount) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

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
    log         *log.Logger

    postsHold   time.Duration
    readHold    int
    maxFeedReq  int
}

func NewStore(file string, log *log.Logger) (*Store, error) {
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
        _, err = tx.CreateBucketIfNotExists([]byte("feedrequests"))
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
    s.log = log

    s.postsHold = time.Hour * 24 * 2
    s.readHold = 128
    s.maxFeedReq = 64

    return &s, nil
}

func (s *Store) Dump() {
    s.db.View(func (tx *bolt.Tx) error {
        s.log.Printf("Bucket 'feeds':")
        b := tx.Bucket([]byte("feeds"))
        c := b.Cursor()
        for k, v := c.First(); k != nil; k, v = c.Next() {
            var feed Feed
            id := int64(binary.BigEndian.Uint64(k))
            err := json.Unmarshal(v, &feed)
            if err == nil {
                s.log.Printf("  %d = %s", id, feed)
            } else {
                s.log.Printf("  %d = INVALID JSON", id)
            }
        }
        return nil
    })
}

func (s *Store) Close() {
    s.db.Close()
    return
}

func (s *Store) PostsMaxAge() time.Time {
    return time.Now().Add(s.postsHold * -1)
}

func (s *Store) CheckVersion() string {
    version := "?"
    s.db.View(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("info"))
        if b == nil {
            version = "0.1"
            return nil
        }
        v := b.Get([]byte("dbversion"))
        if v == nil {
            return nil
        }
        version = string(v)
        return nil
    })
    return version
}

func (s *Store) UpdateDB() error {
    if s.CheckVersion() == "?" {
        return errors.New("Unknown database version")
    }
    if s.CheckVersion() == "0.1" {
        log.Printf("updating db 0.1 -> 0.2")
        err := s.db.Update(func (tx *bolt.Tx) error {
            err := tx.DeleteBucket([]byte("guids"))
            if err != nil {
                return err
            }
            b, err := tx.CreateBucket([]byte("info"))
            if err != nil {
                return err
            }
            err = b.Put([]byte("dbversion"), []byte("0.2"))
            if err != nil {
                return err
            }
            return nil
        })
        if err != nil {
            return err
        }
    }
    log.Printf("all pending db updates finished")
    return nil
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
                s.log.Printf("ERROR UNMARSHALING FEED")
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
            post.Date = TimeFromID(post.ID)
            s.posts = append(s.posts, &post)
            s.postMap[post.ID] = &post
        }
        return nil
    })
}

func (s *Store) PostsInsert(posts []*Post) error {
    if len(posts) == 0 {
        return nil
    }

    maxAge := s.PostsMaxAge()

    err := s.db.Update(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("posts"))
        for _, p := range(posts) {
            if p.Date.Before(maxAge) {
                continue
            }
            v, err := json.Marshal(p)
            if err != nil {
                continue
            }
            var k [8]byte
            binary.BigEndian.PutUint64(k[:], uint64(p.ID))
            b.Put(k[:], v)
        }
        return nil
    })
    if err != nil {
        s.log.Printf("ERROR: %s", err.Error())
    }
    s.postCacheInvalidate()
    return err
}

func (s *Store) PostsGUIDMap() (map[string]bool, error) {
    guids := make(map[string]bool)
    err := s.db.Update(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("posts"))
        c := b.Cursor()
        for k, v := c.First(); k != nil; k, v = c.Next() {
            var post Post
            err := json.Unmarshal(v, &post)
            if err != nil {
                return err
            }
            guids[post.GUID] = true
        }
        return nil
    })
    if err != nil {
        return nil, err
    }
    return guids, nil
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
        t := MakeIDRaw(s.PostsMaxAge(), 0, 0)
        b := tx.Bucket([]byte("posts"))
        guids := tx.Bucket([]byte("guids"))
        c := b.Cursor()
        var start [8]byte
        binary.BigEndian.PutUint64(start[:], uint64(t))
        // Seek here, than do Prev right after to skip first value
        c.Seek(start[:])
        for k, v := c.Prev(); k != nil; k, v = c.Prev() {
            err := b.Delete(k)
            if err != nil {
                s.log.Printf("WARNING: UNABLE TO TRIM DATABASE ELEMENT")
                continue
            }
            var post Post
            err = json.Unmarshal(v, &post)
            if err != nil {
                s.log.Printf("WARNING: UNABLE TO TRIM DATABASE ELEMENT")
                continue
            }
            err = guids.Delete([]byte(post.GUID))
            if err != nil {
                s.log.Printf("WARNING: UNABLE TO TRIM DATABASE ELEMENT")
                continue
            }
            n += 1
        }
        return nil
    })
    s.log.Printf("trimmed %d posts", n)
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

/******************************************************************************
 * FEED REQUESTS
 *****************************************************************************/

func (s *Store) FeedReqsAdd(url string) error {
    if url == "" { return errors.New("invalid feed request url") }

    // add suggestion to database
    err := s.db.Update(func (tx *bolt.Tx) error {
        var err error
        b := tx.Bucket([]byte("feedrequests"))
        encoded := b.Get([]byte(url))
        // case 1: request already exists
        if encoded != nil {
            var req FeedReq
            err := json.Unmarshal(encoded, &req)
            if err != nil {
                return errors.New("unable to encode json")
            }
            // update fields
            req.N += 1
            req.Date = time.Now()

            encoded, err = json.Marshal(req)
            if err != nil {
                return errors.New("unable to encode json")
            }
        } else {
            // case 2: request does not exist
            if b.Stats().KeyN > s.maxFeedReq {
                return errors.New("maximum number of feed request reached")
            }
            var req FeedReq
            req.ID = MakeID()
            req.URL = url
            req.Date = time.Now()
            req.N = 1
            encoded, err = json.Marshal(req)
            if err != nil {
                return errors.New("unable to encode json")
            }
        }
        b.Put([]byte(url), encoded)
        return nil
    })
    return err
}

func (s *Store) FeedReqsAll() ([]*FeedReq, error) {
    res := make([]*FeedReq, 0)
    err := s.db.View(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("feedrequests"))
        c := b.Cursor()
        for k, v := c.First(); k != nil; k, v = c.Next() {
            var f FeedReq
            err := json.Unmarshal(v, &f)
            if err != nil {
                continue
            }
            res = append(res, &f)
        }
        return nil
    })

    return res, err
}

func (s *Store) FeedReqsRemove(fs []*FeedReq) error {
    err := s.db.Update(func (tx *bolt.Tx) error {
        b := tx.Bucket([]byte("feedrequests"))
        for _, f := range(fs) {
            b.Delete([]byte(f.URL))
        }
        return nil
    })
    return err
}


func (s *Store) FeedReqsRemoveAll() error {
    err := s.db.Update(func (tx *bolt.Tx) error {
        tx.DeleteBucket([]byte("feedrequests"))
        tx.CreateBucketIfNotExists([]byte("feedrequests"))
        return nil
    })
    return err
}
