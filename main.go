package main;

import (
    "time"
    "log"
    "flag"
    "os"
    "fmt"
    "strings"
    "sort"
    "html/template"
    "encoding/json"
    "encoding/hex"
    "errors"
    "crypto/sha1"
    "bytes"

    "golang.org/x/crypto/ssh/terminal"
    "github.com/gin-gonic/gin"
    )


const perPage int = 50
const dbFile string = "./data.bolt"
const configFile string = "./config.json"

func loadHTMLGlob(engine *gin.Engine, pattern string) {
    funcMap := template.FuncMap{
        "hashID": HashID,
        "dateFormat": func (t time.Time) string { return t.Format("2006-01-02 15:04:05") },
    }
    templ := template.Must(template.New("").Funcs(funcMap).ParseGlob(pattern))
    engine.SetHTMLTemplate(templ)
}

type config struct {
    BaseURL string  `json:"baseUrl,omitempty"`
    Addr    string  `json:"address,omitempty"`
    HashHex string  `json:"passwd,omitempty"`
}

func loadConfig(filename string) (*config, error) {
    file, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    defer file.Close()
    decoder := json.NewDecoder(file)
    res := &config{"", ":8080", ""}
    err = decoder.Decode(res)
    if err != nil {
        return nil, err
    }
    return res, nil
}

func cmdRun() error {

    var err error

    config, err := loadConfig(configFile)
    if err != nil {
        return err;
    }
    if config.HashHex == "" {
        return errors.New("NO PASSWORD HASH SPECIFIED")
    }

    baseURL := config.BaseURL
    addr := config.Addr
    hash, err := hex.DecodeString(config.HashHex)
    if err != nil {
        return errors.New("invalid hex string for password hash")
    }

    /* START SUBSYSTEMS */

    store, err := NewStore(dbFile)
    if err != nil {
        return err;
    }
    defer store.Close()

    feedd := NewFeedD(store)
    feedd.Start()
    defer feedd.Stop()

    articled := NewArticleD(store)
    articled.Start()
    defer articled.Stop()

    /* PERIODICALLY TRIM ARTICLES */
    stoptrim := make(chan bool, 1)
    go func (stop chan bool) {
        for true {
            select {
            case <-stop:
                return;
            case <-time.After(time.Minute * 60):
                break;
            }
            // Trim articles older than 2 days
            store.TrimByTime(time.Now().Add(time.Hour * 24 * -2))
        }
    }(stoptrim)
    defer func() {
        stoptrim <- true
    }()

    /* CONFIGURE SERVER */
    r := gin.Default()

    loadHTMLGlob(r, "./templates/*")

    r.Static(baseURL + "/static", "./static")

    /*   /   - INDEX */
    r.GET(baseURL + "/", func(c *gin.Context) {
        c.HTML(200, "index.tmpl", gin.H{"base": baseURL})
    })



    /*   /f/ - NEWS */

    r.GET(baseURL + "/f/", func(c *gin.Context) {
        posts := store.PostsAll(perPage)
        feedsMap := store.FeedsAllMap()
        c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap, "base": baseURL})
    })
    r.GET(baseURL + "/f/:feeds", func(c *gin.Context) {
        if c.Param("feeds") == "all" || c.Param("feeds") == "" {
            posts := store.PostsAll(perPage)
            feedsMap := store.FeedsAllMap()
            c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap, "base": baseURL})
        } else {
            feedlist := strings.Split(c.Param("feeds"), "+")
            posts := store.PostsByFeeds(perPage, feedlist)
            log.Printf("found %d posts", len(posts))
            feedsMap := store.FeedsAllMap()
            c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap, "base": baseURL})
        }
    })

    /*   /l/ - FEED LIST */

    r.GET(baseURL + "/l/", func(c *gin.Context) {
        feeds := store.FeedsAll()
        c.HTML(200, "feeds.tmpl", gin.H{"feeds": feeds, "base": baseURL})
    })


    /*   /a/*- ARTICLES */

    r.GET(baseURL + "/a/:articleid", func(c *gin.Context) {
        //postID, err := strconv.ParseInt(c.Param("articleid"), 10, 64)
        postID := UnhashID(c.Param("articleid"))
        if err != nil { c.String(200, err.Error()); return }
        post := store.PostsID(postID)
        if post == nil { c.String(200, fmt.Sprintf("invalid article: %d", postID)); return }
        content, err := articled.GetArticleContent(postID)
        if err != nil { c.String(200, err.Error()); return }
        c.HTML(200, "article.tmpl", gin.H{"title": post.Title, "content": template.HTML(content), "base": baseURL})
    });

    /*   /c/*- CONTROL CENTER */

    r.GET(baseURL + "/c/", func(c *gin.Context) {
        stats := make(map[string]interface{})
        stats["num posts"] = len(store.PostsAll(-1))
        c.HTML(200, "control.tmpl", gin.H{"stats": stats})
    });

    r.POST(baseURL + "/c/", func(c *gin.Context) {
        passwdHex := c.PostForm("passwd")
        key := c.PostForm("cmd")
        val := c.PostForm("arg")
        passwd, err := hex.DecodeString(passwdHex)
        if err != nil {
            c.String(200, "Invalid password")
            return
        }
        thisHash := sha1.Sum(passwd)
        if bytes.Compare(thisHash[:], hash) != 0 {
            c.String(200, "Invalid password")
            return
        }
        c.String(200, key + " = " + val)
    })

    /*   /x/*- APIs */

    r.GET(baseURL + "/x/a/:articleid", func(c *gin.Context) {
        //postID, err := strconv.ParseInt(c.Param("articleid"), 10, 64)
        postID := UnhashID(c.Param("articleid"))
        if err != nil { c.String(200, err.Error()); return }
        post := store.PostsID(postID)
        if post == nil { c.String(200, fmt.Sprintf("invalid article: %d", postID)); return }
        content, err := articled.GetArticleContent(postID)
        if err != nil { c.String(200, err.Error()); return }
        c.String(200, content);
    });

    /*

    r.GET("/suggest", func(c *gin.Context) {
        c.String(http.StatusOK, "Not Implemented yet");
    })
    */

    r.Run(addr)

    return nil
}

func cmdCmd() error {
    flag.Parse()
    if flag.NArg() < 2 {
        return help()
    }
    return errors.New("Not yet implemented")
}

func cmdHash() error {
    oldState, err := terminal.MakeRaw(0)
    if err != nil {
        return err
    }
    defer terminal.Restore(0, oldState)
    term := terminal.NewTerminal(os.Stdin, "")
    pw, err := term.ReadPassword("Enter password: ")
    if err != nil { return err }
    hash := sha1.Sum([]byte(pw))
    log.Printf("password Hash: %s", hex.EncodeToString(hash[:]))
    return nil
}

type int64Slice []int64
func (s int64Slice) Len() int { return len(s) }
func (s int64Slice) Less(i, j int) bool { return s[i] < s[j] }
func (s int64Slice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func cmdTest() error {
    store, err := NewStore(dbFile)
    if err != nil {
        return err
    }
    defer store.Close()

    store.Dump()
    return nil
}

func cmdTestIDGen() error {
    n := 10
    m := 2000
    ids := make([]*IDGen, n)
    buf := make([]int64, n*m)
    for i := 0; i < n; i += 1 {
        ids[i] = NewIDGen(i+1)
    }
    for i := 0; i < n; i += 1 {
        for j := 0; j < m; j += 1 {
            buf[i*m + j] = ids[i].MakeID()
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

    store, err := NewStore(dbFile)
    if err != nil {
        return err
    }
    defer store.Close()

    feedd := NewFeedD(store)
    feedd.Start()
    defer feedd.Stop()

    time.Sleep(time.Minute * 10)

    return nil
}

func cmdInit() error {
    var err error
    os.Remove(dbFile)
    store, err := NewStore(dbFile)
    if err != nil { return err }
    defer store.Close()
    return nil
}

func cmdInitDebug() error {
    var err error

    os.Remove(dbFile)
    store, err := NewStore(dbFile)
    if err != nil {
        return err
    }
    defer store.Close()

    var feed Feed

    log.Printf("Adding feeds")
    feed.ID = MakeID()
    feed.URL = "http://feeds.bbci.co.uk/news/rss.xml"
    feed.Handle = "bbc"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://feeds.bbci.co.uk/news/world/europe/rss.xml"
    feed.Handle = "bbce"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://rss.csmonitor.com/feeds/csm"
    feed.Handle = "csm"
    if err = store.FeedsSet(&feed); err != nil { return err }

    return nil
}

func help() error {
    log.Printf("Usage:")
    log.Printf("    go-news command")
    log.Printf("")
    log.Printf("Commands are:")
    log.Printf("    run         run news web app normally")
    log.Printf("    cmd <key>=<value>")
    log.Printf("                send command to running web app")
    log.Printf("    hash        generate hash for database")
    log.Printf("    init        (re-)initialize database")
    log.Printf("    initDbg     (re-)initialize database (fill with debug values)")
    log.Printf("    test        arbitray test code")
    log.Printf("    testFeeds   run rss daemon for a while")
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
    case cmdStr == "cmd":
        cmdFunc = cmdCmd
    case cmdStr == "hash":
        cmdFunc = cmdHash
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
    HashIDInit()
    if err := cmdFunc(); err != nil {
        log.Fatal(err.Error())
    }
}
