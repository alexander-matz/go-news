package main;

import (
    "time"
    "log"
    "flag"
    "html/template"
    _ "strings"

    "github.com/gin-gonic/gin"
    _ "fmt"
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

    /*
    r.GET("/f/:feeds", func(c *gin.Context) {
    })

    r.GET("/a/:articlehash", func(c *gin.Context) {
        hash := c.Param("articlehash")
        if post, ok := PostsMap[hash]; ok {
            c.HTML(http.StatusOK, "article.tmpl", gin.H{"title": post.Title, "content": "No content supported yet"})
        } else {
            c.String(http.StatusBadRequest, fmt.Sprintf("Invalid article: %s", hash));
            return
        }
    });


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

func cmdTest() error {
    /*
    var num int64
    var hash string
    num = 1
    hash = store.HashId(num)
    log.Printf("num: %d, hash: %s", num, hash)

    var err error

    if err = store.Init(); err != nil { return err }
    defer store.Deinit()

    if err = feeds.Init(); err != nil { return err }
    defer feeds.Deinit()

    //time.Sleep(time.Minute * 30)

    */
    time.Sleep(time.Second * 1)
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
    InitId()
    if err := cmdFunc(); err != nil {
        log.Fatal(err.Error())
    }
}
