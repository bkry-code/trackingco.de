package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"

	"github.com/fjl/go-couchdb"
	"github.com/galeone/igor"
	"github.com/kelseyhightower/envconfig"
	"github.com/speps/go-hashids"
	napping "gopkg.in/jmcvetta/napping.v3"
	mailgun "gopkg.in/mailgun/mailgun-go.v1"
	"gopkg.in/redis.v5"
)

type Settings struct {
	Host                    string `envconfig:"HOST" required:"true"`
	Port                    string `envconfig:"PORT" required:"true"`
	CouchURL                string `envconfig:"COUCH_URL" required:"true"`
	CouchDatabaseName       string `envconfig:"COUCH_DATABASE" required:"true"`
	RedisAddr               string `envconfig:"REDIS_ADDR" required:"true"`
	RedisPassword           string `envconfig:"REDIS_PASSWORD" required:"true"`
	PostgresURL             string `envconfig:"DATABASE_URL" required:"true"`
	BitcoinPayApiKey        string `envconfig:"BITCOINPAY_KEY"`
	SessionOffsetHashidSalt string `envconfig:"SESSION_OFFSET_HASHID_SALT"`
	LoggedAs                string `envconfig:"LOGGED_AS"`
	Auth0Secret             string `envconfig:"AUTH0_SECRET"`
	HerokuToken             string `envconfig:"HEROKU_TOKEN"`
	HerokuAppName           string `envconfig:"HEROKU_APPNAME"`
	MailgunDomain           string `envconfig:"MAILGUN_DOMAIN"`
	MailgunApiKey           string `envconfig:"MAILGUN_API_KEY"`
}

var err error
var s Settings
var b napping.Session
var pg *igor.Database
var mg mailgun.Mailgun
var hso *hashids.HashID
var rds *redis.Client
var couch *couchdb.DB
var tracklua string
var blacklist map[string]bool

func main() {
	err = envconfig.Process("", &s)
	if err != nil {
		log.Fatal("couldn't process envconfig: ", err)
	}

	// mailgun
	mg = mailgun.NewMailgun(s.MailgunDomain, s.MailgunApiKey, "")

	// redis
	rds = redis.NewClient(&redis.Options{
		Addr:     s.RedisAddr,
		Password: s.RedisPassword,
	})

	// postgres
	pg, err = igor.Connect(s.PostgresURL)
	if err != nil {
		log.Fatal("couldn't connect to postgres at "+s.PostgresURL+": ", err)
	}

	// couchdb
	couchS, err := couchdb.NewClient(s.CouchURL, nil)
	if err != nil {
		log.Fatal("failed to created couchdb client: ", err)
	}
	couch = couchS.DB(s.CouchDatabaseName)

	// bitcoinpay client
	b = napping.Session{Header: &http.Header{
		"Authorization": []string{
			"Token " + s.BitcoinPayApiKey,
		},
		"Content-Type": []string{
			"application/json",
		},
	}}

	// hashids for session offset
	hd := hashids.NewData()
	hd.Salt = s.SessionOffsetHashidSalt
	hso = hashids.NewWithData(hd)

	// track.lua
	filename := "./track.lua"
	// try the current directory
	btracklua, err := ioutil.ReadFile(filename)
	if err != nil {
		// try some magic (based on the path of the source main.go)
		_, this, _, _ := runtime.Caller(0)
		here := path.Dir(this)
		btracklua, err = ioutil.ReadFile(filepath.Join(here, filename))
		if err != nil {
			log.Fatal("failed to read track.lua: ", err)
		}
	}
	tracklua = string(btracklua)

	// referrer blacklist
	blacklist = buildReferrerBlacklist()
	log.Print("using referrer blacklist with ", len(blacklist), " entries.")

	// logged as
	if s.LoggedAs != "" {
		log.Print("logged by default as ", s.LoggedAs)
	}

	// run routines or start the server
	if len(os.Args) == 1 {
		runServer()
	} else {
		switch os.Args[1] {
		case "daily":
			daily()
		case "every8days":
			every8days()
		case "monthly":
			monthly()
		default:
			log.Print("couldn't find what to run for ", os.Args[1])
		}
	}
}
