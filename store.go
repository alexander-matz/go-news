package main;

import (
    "sync"
    "time"
    "errors"
    "os"
    "encoding/json"
    "sort"
    )

type Feed struct {
    Id          int64   `json:"id"`
    Initialized bool    `json:"initialized"`
    Handle      string  `json:"handle"`
    Title       string  `json:"title"`
    Link        string  `json:"link"`
    Url         string  `json:"url"`
    ImageUrl    string  `json:"imageurl"`
}

type Post struct {
    Id      int64
    Title   string
    Guid    string
    Link    string
    Feed    int64
    Date    time.Time
}
type postByDate []*Post
func (p postByDate) Len() int { return len(p) }
// Sorting by date via id comparison only works because we're using
// twitter snowflake ids
func (p postByDate) Less(i, j int) bool { return p[i].Id < p[j].Id }
func (p postByDate) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

type Store struct {
    nextFId     int64
    nextPId     int64
    feeds       []*Feed
    posts       []*Post
    feedMap     map[int64]*Feed
    postMap     map[int64]*Post
    guidMap     map[string]*Post
    flock       sync.Mutex
    plock       sync.Mutex
}

func NewStore() *Store {
    var s Store
    s.feeds = make([]*Feed, 0)
    s.posts = make([]*Post, 0)
    s.feedMap = make(map[int64]*Feed)
    s.postMap = make(map[int64]*Post)
    s.guidMap = make(map[string]*Post)

    return &s
}

func (s *Store) LoadFromFile(filename string) error {
    s.flock.Lock()
    s.plock.Lock()
    defer s.flock.Unlock()
    defer s.plock.Unlock()

    s.feeds = make([]*Feed, 0)
    s.posts = make([]*Post, 0)
    s.feedMap = make(map[int64]*Feed)
    s.postMap = make(map[int64]*Post)
    s.guidMap = make(map[string]*Post)

    file, err := os.Open(filename)
    if err != nil { return err }
    defer file.Close()
    parser := json.NewDecoder(file)
    err = parser.Decode(&s.feeds)
    if err != nil { return err }

    for _, feed := range(s.feeds) {
        s.feedMap[feed.Id] = feed
    }
    return nil
}

func (s *Store) SaveToFile(filename string) error {
    s.flock.Lock()
    s.plock.Lock()
    defer s.flock.Unlock()
    defer s.plock.Unlock()

    file, err := os.Create(filename)
    if err != nil { return err }
    defer file.Close()
    encoder := json.NewEncoder(file)
    err = encoder.Encode(s.feeds)
    if err != nil { return err }
    return nil
}

func (s *Store) Close() {
    return
}

func (s *Store) getFId() int64 {
    var res = s.nextFId
    s.nextFId += 1
    return res
}

func (s *Store) getPId() int64 {
    var res = s.nextPId
    s.nextPId += 1
    return res
}

func (s *Store) FeedsAdd(f *Feed) error {
    if f.Url == "" { return errors.New("invalid feed url") }
    if f.Handle == "" { return errors.New("invalid feed handle") }

    s.flock.Lock()
    defer s.flock.Unlock()

    for _, feed := range(s.feeds) {
        if feed.Url == f.Url { return errors.New("feed url already exists") }
        if feed.Handle == f.Handle { return errors.New("feed handle already exists") }
    }
    newFeed := &Feed{MakeId(), false, f.Handle, "", "", f.Url, ""}

    s.feeds = append(s.feeds, newFeed)
    s.feedMap[newFeed.Id] = newFeed
    return nil
}

func (s *Store) FeedsUpdate(f *Feed) error {
    feed, ok := s.feedMap[f.Id];
    if !ok { return errors.New("feed does not exist") }
    s.flock.Lock()
    defer s.flock.Unlock()
    feed.Initialized = true
    feed.Handle = f.Handle
    feed.Title = f.Title
    feed.Link = f.Link
    feed.Url = f.Url
    feed.ImageUrl = f.ImageUrl
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
        res[v.Id] = v
    }
    return res
}

func (s *Store) PostsInsertOrIgnore(posts []*Post) error {
    s.plock.Lock()
    defer s.plock.Unlock()
    for _, p := range(posts) {
        _, ok := s.guidMap[p.Guid];
        if !ok {
            newPost := &Post{p.Id, p.Title, p.Guid, p.Link, p.Feed, p.Date}
            s.posts = append(s.posts, newPost)
            s.guidMap[newPost.Guid] = newPost
            s.postMap[newPost.Id] = newPost
        }
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

func (s *Store) PostsId(id int64) *Post {
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
        delete(s.guidMap, s.posts[i].Guid)
        delete(s.postMap, s.posts[i].Id)
        s.posts[i] = nil
    }
    s.posts = s.posts[:cutPoint]
}

func (s *Store) TrimByNumber(n int) {
    s.plock.Lock()
    defer s.plock.Unlock()
    if (n > len(s.posts)) { return }
    for i := n; i < len(s.posts); i += 1 {
        delete(s.guidMap, s.posts[i].Guid)
        delete(s.postMap, s.posts[i].Id)
        s.posts[i] = nil
    }
    s.posts = s.posts[:n]
}
