package main;

import (
    "time"
    "log"
    "flag"
    "html/template"

    "github.com/gin-gonic/gin"
    "fmt"
    "strconv"
    "strings"
    "sort"
    )


const perPage int = 50
const configFile string = "./config.json"

func loadHTMLGlob(engine *gin.Engine, pattern string) {
    funcMap := template.FuncMap{
        "hashId": HashId,
    }
    templ := template.Must(template.New("").Funcs(funcMap).ParseGlob(pattern))
    engine.SetHTMLTemplate(templ)
}

func cmdRun() error {

    var err error

    store := NewStore()
    err  = store.LoadFromFile(configFile)
    if err != nil { return err; }
    defer store.Close()

    feedd := NewFeedD(store)
    feedd.Start()
    defer feedd.Stop()

    articled := NewArticleD(store)
    articled.Start()
    defer articled.Stop()

    r := gin.Default()

    loadHTMLGlob(r, "./templates/*")

    r.Static("/static", "./static")

    r.GET("/", func(c *gin.Context) {
        posts := store.PostsAll(perPage)
        feedsMap := store.FeedsAllMap()
        feeds := make([]*Feed, len(posts))
        for i, p := range(posts) {
            feeds[i] = feedsMap[p.Feed]
        }
        c.HTML(200, "index.tmpl", gin.H{"posts": posts, "feeds": feeds})
    })

    r.GET("/f/:feeds", func(c *gin.Context) {
        feedlist := strings.Split(c.Param("feeds"), "+")
        posts := store.PostsFeeds(perPage, feedlist)
        log.Printf("found %d posts", len(posts))
        feedsMap := store.FeedsAllMap()
        feeds := make([]*Feed, len(posts))
        for i, p := range(posts) {
            feeds[i] = feedsMap[p.Feed]
        }
        c.HTML(200, "index.tmpl", gin.H{"posts": posts, "feeds": feeds})
    })

    r.GET("/a/:articleid", func(c *gin.Context) {
        postid, err := strconv.ParseInt(c.Param("articleid"), 10, 64)
        if err != nil { c.String(200, err.Error()) }
        post := store.PostsId(postid)
        if post == nil { c.String(200, fmt.Sprintf("Invalid article: %d", postid)) }
        content, err := articled.GetArticleContent(postid)
        if err != nil { c.String(200, err.Error()) }
        c.HTML(200, "article.tmpl", gin.H{"title": post.Title, "content": template.HTML(content)})
    });

    /*

    r.GET("/feeds", func(c *gin.Context) {
        c.HTML(http.StatusOK, "feeds.tmpl", gin.H{"feeds": FeedsSorted})
    })

    r.GET("/suggest", func(c *gin.Context) {
        c.String(http.StatusOK, "Not Implemented yet");
    })
    */

    r.Run(":8080")

    return nil
}

type int64Slice []int64
func (s int64Slice) Len() int { return len(s) }
func (s int64Slice) Less(i, j int) bool { return s[i] < s[j] }
func (s int64Slice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func cmdTest() error {
    n := 10
    m := 2000
    ids := make([]*IdGen, n)
    buf := make([]int64, n*m)
    for i := 0; i < n; i += 1 {
        ids[i] = NewIdGen(i+1)
    }
    for i := 0; i < n; i += 1 {
        for j := 0; j < m; j += 1 {
            buf[i*m + j] = ids[i].MakeId()
        }
    }
    sort.Sort(int64Slice(buf))
    duplicates := 0
    for i := 0; i < n-1; i += 1 {
        if buf[i] == buf[i+1] {
            duplicates += 1
        }
    }
    log.Printf("found %d duplicates", duplicates)

    return nil
}

func cmdTestFeeds() error {
    var err error

    store := NewStore()
    err = store.LoadFromFile(configFile)
    if err != nil { return err }

    feedd := NewFeedD(store)
    feedd.Start()
    defer feedd.Stop()

    time.Sleep(time.Minute * 10)

    return nil
}

func cmdInit() error {
    var err error
    store := NewStore()
    err = store.SaveToFile(configFile)
    if err != nil { return err }
    return nil
}

func cmdInitDebug() error {
    var err error
    store := NewStore()

    var feed Feed

    log.Printf("Adding feeds")
    feed.Url = "http://feeds.bbci.co.uk/news/rss.xml"
    feed.Handle = "bbc"
    if err = store.FeedsAdd(&feed); err != nil { return err }

    feed.Url = "http://feeds.bbci.co.uk/news/world/europe/rss.xml"
    feed.Handle = "bbce"
    if err = store.FeedsAdd(&feed); err != nil { return err }

    feed.Url = "http://rss.csmonitor.com/feeds/csm"
    feed.Handle = "csm"
    if err = store.FeedsAdd(&feed); err != nil { return err }

    log.Printf("Saving store")
    err = store.SaveToFile(configFile)
    if err != nil { return err }

    return nil
}

func help() error {
    log.Printf("Usage:\n")
    log.Printf("    go-news command\n")
    log.Printf("\n")
    log.Printf("Commands are:\n")
    log.Printf("    run         run news web app normally\n")
    log.Printf("    init        (re-)initialize database\n")
    log.Printf("    initDbg     (re-)initialize database (fill with debug values)\n")
    log.Printf("    test        arbitray test code\n")
    log.Printf("    testFeeds   run rss daemon for a while\n")
    return nil
}

func main() {
    flag.Parse()
    if flag.NArg() < 1 {
        help()
        return
    }
    cmdStr := flag.Arg(0)
    var cmdFunc func() error
    switch {
    case cmdStr == "run":
        cmdFunc = cmdRun
    case cmdStr == "init":
        cmdFunc = cmdInit
    case cmdStr == "initDbg":
        cmdFunc = cmdInitDebug
    case cmdStr == "test":
        cmdFunc = cmdTest
    case cmdStr == "testFeeds":
        cmdFunc = cmdTestFeeds
    case true:
        cmdFunc = help
    }
    HashIdInit()
    if err := cmdFunc(); err != nil {
        log.Fatal(err.Error())
    }
}
