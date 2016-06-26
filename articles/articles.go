package main;


import (
    "io/ioutil"
    "net/http"
    "sync"
    "github.com/mauidude/go-readability"
    )

var content = make(map[string]string)
var contentMutex sync.Mutex

func idToUrl(id string) string {
    return id
}

func urlToId(url string) string {
    return url
}

func ArticleGetContent(url string) (string, error) {
    id := urlToId(url)
    if data, ok := content[id]; ok {
        return data, nil
    } else {
        var err error
        var res *http.Response
        var doc *readability.Document
        var html []byte
        res, err = http.Get(url);
        if err != nil { return "", err }

        defer res.Body.Close()
        html, err = ioutil.ReadAll(res.Body)
        if err !=  nil { return "", err }

        doc, err = readability.NewDocument(string(html));
        if err != nil { return "", err }

        contentMutex.Lock()
        content[id] = doc.Content()
        contentMutex.Unlock()
        return doc.Content(), nil
    }
}
