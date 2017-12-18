package db;

import (
	"time"
	"strings"
	"fmt"
	"errors"
	"database/sql"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	_ "github.com/lib/pq"
	_ "github.com/ziutek/mymysql"
)

type Feed struct {
	ID     int64  `db:"id"`
	Handle string `db:"handle"`
	Title  string `db:"title"`
	Link   string `db:"link"`
	URL    string `db:"url"`
}

type Post struct {
	ID      int64      `db:"id"`
	Title   string     `db:"title"`
	GUID    string     `db:"guid"`
	Link    string     `db:"link"`
	Feed    int64      `db:"feed"`
	Date    time.Time  `db:"time"`
	Content string     `db:"content"`
}

type FeedReq struct {
	ID   int64
	URL  string
	N    int
	Date time.Time
}

type DB struct {
	db *sqlx.DB
}

var schema = `
CREATE TABLE IF NOT EXISTS settings (
	name TEXT PRIMARY KEY UNIQUE,
	value TEXT 
);
CREATE TABLE IF NOT EXISTS feeds (
	id INTEGER PRIMARY KEY NOT NULL,
	handle TEXT UNIQUE,
	title TEXT,
	link TEXT,
	url TEXT UNIQUE
);
CREATE TABLE IF NOT EXISTS posts (
	id INTEGER PRIMARY KEY NOT NULL,
	title TEXT,
	guid TEXT UNIQUE,
	link TEXT,
	feed INTEGER,
	time DATETIME,
	content TEXT,
	FOREIGN KEY(feed) REFERENCES feeds(id)
);
CREATE TABLE IF NOT EXISTS requests (
	id INTEGER PRIMARY KEY NOT NULL,
	url TEXT,
	num INTEGER
);
`
///////////////////////////////////////////////////////////
// general functionality

func Connect(url string) (*DB, error) {
	if url == "" {
		return nil, errors.New("invalid connection string")
	}
	parts := strings.SplitN(url, "://", 2)
	if len(parts) < 2 {
		return nil, errors.New("conneciton string must be <driver>://<source>")
	}

	var (
		db *sqlx.DB
		err error
	)

	if db, err = sqlx.Connect(parts[0], parts[1]); err != nil {
		return nil, err
	}

	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}

	return &DB{db}, nil
}

func (db *DB) Disconnect() {
	if err := db.db.Close(); err != nil {
		panic(err)
	}
	db.db = nil
}

///////////////////////////////////////////////////////////
// feed management

// ID     int64  `db:"id"`
// Handle string `db:"handle"`
// Title  string `db:"title"`
// Link   string `db:"link"`
// URL    string `db:"url"`

func (db *DB) FeedAdd(feed *Feed) (int64, error) {
	query := "INSERT INTO feeds(handle, url) VALUES (:handle, :url)"
	if res, err := db.db.NamedExec(query, feed); err != nil {
		return -1, err
	} else if id, err := res.LastInsertId(); err != nil {
		return -1, err
	} else {
		feed.ID = id
		return id, nil
	}
}

func (db *DB) FeedUpdate(feed *Feed) error {
	query := "UPDATE feeds SET handle = :handle, URL = :url, title = :title, link = :link WHERE id = :id"
	res, _ := db.db.NamedExec(query, feed)
	if nrows, err := res.RowsAffected(); err != nil {
		return err
	} else if nrows == 0 {
		return errors.New("invalid feed")
	} else {
		return nil
	}
}

func (db *DB) FeedRemoveByHandleOrURL(handleOrURL string) error {
	query := "DELETE FROM feeds WHERE handle = $1 or url = $1";
	res, _ := db.db.Exec(query, handleOrURL)
	if nrows, err := res.RowsAffected(); err != nil {
		return err
	} else if nrows == 0 {
		return errors.New("no feed with that handle or url")
	} else {
		return nil
	}
}

func (db *DB) FeedAll() ([]*Feed, error) {
	query := `SELECT * FROM feeds;`
	feeds := []*struct{
		ID int64
		Handle string
		Title  sql.NullString
		Link   sql.NullString
		URL    string
	}{}
	if err := db.db.Select(&feeds, query); err != nil {
		return nil, err
	}
	feedsReal := []*Feed{}
	for _, f := range(feeds) {
		feedsReal = append(feedsReal, &Feed{f.ID, f.Handle, f.Title.String, f.Link.String, f.URL})
	}
	return feedsReal, nil
}


///////////////////////////////////////////////////////////
// post management

// ID      int64      `db:"id"`
// Title   string     `db:"title"`
// GUID    string     `db:"guid"`
// Link    string     `db:"link"`
// Feed    int64      `db:"feed"`
// Date    time.Time  `db:"time"`
// Content string     `db:"content"`

func (db *DB) PostAdd(post *Post) (int64, error) {
	query := `REPLACE INTO posts(title, guid, link, feed, time)
		VALUES (:title, :guid, :link, :feed, :time)`
	if res, err := db.db.NamedExec(query, post); err != nil {
		return -1, err
	} else if id, err := res.LastInsertId(); err != nil {
		return -1, err
	} else {
		post.ID = id
		return id, nil
	}
}

func (db *DB) PostAddBatch(posts []*Post) error {
	nerr := 0
	for _, post := range(posts) {
		_, err := db.PostAdd(post)
		if err != nil {
			nerr += 1
		}
	}
	if nerr > 0 {
		return errors.New(fmt.Sprintf("failed to add %d posts", nerr))
	} else {
		return nil
	}
}

func (db *DB) PostNAfter(n int, after time.Time) ([]*Post, error) {
	query := `SELECT id,title,guid,link,feed,date FROM posts WHERE time < $2 LIMIT $1 ORDER BY time DESC;`
	posts := []*struct{
		ID     int64
		Title  sql.NullString
		GUID   sql.NullString
		Link   sql.NullString
		Feed   int64
		Date   time.Time
	}{}
	if err := db.db.Select(&posts, query, n, after); err != nil {
		return nil, err
	}
	postsReal := []*Post{}
	for _, p := range(posts) {
		postsReal = append(postsReal, &Post{
			p.ID,
			p.Title.String,
			p.GUID.String,
			p.Link.String,
			p.Feed,
			p.Date,
			"",
		})
	}
	return postsReal, nil
}

///////////////////////////////////////////////////////////
// content management

func (db *DB) PostFetchContent(post *Post) error {
	query := `SELECT content FROM posts WHERE id = $1`
	postContent := struct{
		Content sql.NullString
	}{}
	if err := db.db.Select(&postContent, query, post.ID); err != nil {
		return err
	}
	// post content already fetched, we're good, just update and return
	if postContent.Content.Valid {
		post.Content = postContent.Content.String
		return nil
	}
	// post content not yet fetched, so we have to do that and update the database
	return errors.New("Fetching using readability not yet implemented")
}
