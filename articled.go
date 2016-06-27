package main;

import (
    "sort"
    "sync"
    "time"
    "log"
    "net/http"
    "io/ioutil"
    "errors"
    read "github.com/mauidude/go-readability"
    )

type Content struct {
    Id          int64
    ArticleId   int64
    Content     string
    LastAccess  time.Time
}
type contentByDate []*Content
func (c contentByDate) Len() int { return len(c) }
func (c contentByDate) Less(i, j int) bool { return c[i].LastAccess.Before(c[j].LastAccess) }
func (c contentByDate) Swap(i, j int) { c[i], c[j] = c[j], c[i] }

type ArticleD struct {
    contentMap  map[int64]*Content
    articleMap  map[int64]*Content
    lock        sync.Mutex
    stop        chan bool
    active      bool
    store       *Store
}

func NewArticleD(store *Store) *ArticleD{
    res := &ArticleD{make(map[int64]*Content), make(map[int64]*Content), sync.Mutex{},
                    make(chan bool, 1), false, store}
    return res
}

func (d *ArticleD) Start() {
    if !d.active {
        d.active = true
        go d.run()
    }
}

func (d *ArticleD) Stop() {
    if d.active {
        d.stop <- true
        d.active = false
    }
}

func (d *ArticleD) GetArticleContent(article int64) (string, error) {
    content, ok := d.articleMap[article]
    if ok {
        return content.Content, nil
    } else {
        return d.fetch(article)
    }
}

func (d *ArticleD) run() {
    for true {
        time.Sleep(time.Minute)
        log.Printf("[ArticleD:%s] trimming", nowString())
        d.trim(512)
    }
}

func (d *ArticleD) fetch(article int64) (string, error) {
    post := d.store.PostsId(article)
    if post == nil { return "", errors.New("couldn't find article") }
    res, err := http.Get(post.Link)
    if err != nil { return "", err }
    defer res.Body.Close()
    html, err := ioutil.ReadAll(res.Body)
    if err != nil { return "", err }
    doc, err := read.NewDocument(string(html))
    if err != nil { return "", err }
    content := &Content{MakeId(), post.Id, doc.Content(), time.Now()}
    d.lock.Lock()
    d.contentMap[content.Id] = content
    d.articleMap[content.ArticleId] = content
    d.lock.Unlock()
    return content.Content, nil
}

func (d *ArticleD) trim(n int) {
    d.lock.Lock()
    if len(d.contentMap) < n { return }
    list := make([]*Content, len(d.contentMap))
    sort.Sort(contentByDate(list))
    for i := n; i < len(list); i += 1 {
        delete(d.contentMap, list[i].Id)
        delete(d.articleMap, list[i].ArticleId)
        list[i] = nil
    }
    d.lock.Unlock()
}
