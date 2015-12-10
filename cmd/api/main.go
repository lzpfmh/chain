package main

import (
	"database/sql"
	"expvar"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/kr/secureheader"
	"github.com/resonancelabs/go-pub/instrument"
	"github.com/resonancelabs/go-pub/instrument/client"
	"golang.org/x/net/context"

	"chain/api"
	"chain/api/admin"
	"chain/api/appdb"
	"chain/api/asset"
	"chain/database/pg"
	"chain/env"
	chainlog "chain/log"
	"chain/log/rotation"
	"chain/log/splunk"
	"chain/metrics"
	"chain/metrics/librato"
	chainhttp "chain/net/http"
	"chain/net/http/gzip"
	"chain/net/http/httpspan"
)

var (
	// config vars
	tlsCrt       = env.String("TLSCRT", "")
	tlsKey       = env.String("TLSKEY", "")
	listenAddr   = env.String("LISTEN", ":8080")
	dbURL        = env.String("DB_URL", "postgres:///api?sslmode=disable")
	target       = env.String("STACK", "sandbox") // TODO(kr): rename STACK to TARGET
	samplePer    = env.Duration("SAMPLEPER", 10*time.Second)
	nouserSecret = env.String("NOUSER_SECRET", "")
	splunkAddr   = os.Getenv("SPLUNKADDR")
	logFile      = os.Getenv("LOGFILE")
	logSize      = env.Int("LOGSIZE", 5e6) // 5MB
	logCount     = env.Int("LOGCOUNT", 9)
	logQueries   = env.Bool("LOG_QUERIES", false)
	// for config var LIBRATO_URL, see func init below
	traceguideToken = os.Getenv("TRACEGUIDE_ACCESS_TOKEN")
	maxDBConns      = env.Int("MAXDBCONNS", 10) // set to 100 in prod
	makeBlocks      = env.Bool("MAKEBLOCKS", false)

	// build vars; initialized by the linker
	buildTag    = "dev"
	buildCommit = "?"
	buildDate   = "?"

	race []interface{} // initialized in race.go

	blockInterval = 1 * time.Second
)

func init() {
	librato.URL = env.URL("LIBRATO_URL", "")
	librato.Prefix = "chain.api."
	expvar.NewString("buildtag").Set(buildTag)
	expvar.NewString("builddate").Set(buildDate)
	expvar.NewString("buildcommit").Set(buildCommit)
}

func main() {
	env.Parse()
	sql.Register("schemadb", pg.SchemaDriver(buildTag))
	log.SetPrefix("api-" + buildTag + ": ")
	log.SetFlags(log.Lshortfile)
	chainlog.SetPrefix(append([]interface{}{"app", "api", "target", *target, "buildtag", buildTag}, race...)...)
	chainlog.SetOutput(logWriter())

	if librato.URL.Host != "" {
		librato.Source = *target
		go librato.SampleMetrics(*samplePer)
	} else {
		log.Println("no metrics; set LIBRATO_URL for prod")
	}

	if traceguideToken == "" {
		log.Println("no tracing; set TRACEGUIDE_ACCESS_TOKEN for prod")
	}
	instrument.SetDefaultRuntime(client.NewRuntime(&client.Options{
		AccessToken: traceguideToken,
		GroupName:   "api",
		Attributes: map[string]interface{}{
			"target":      *target,
			"buildtag":    buildTag,
			"builddate":   buildDate,
			"buildcommit": buildCommit,
		},
	}))

	pg.EnableQueryLogging(*logQueries)

	db, err := sql.Open("schemadb", *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(*maxDBConns)
	db.SetMaxIdleConns(100)
	appdb.Init(db)

	var h chainhttp.Handler
	h = api.Handler(*nouserSecret)
	h = metrics.Handler{Handler: h}
	h = gzip.Handler{Handler: h}
	h = httpspan.Handler{Handler: h}

	bg := context.Background()
	bg = pg.NewContext(bg, db)
	if *makeBlocks {
		admin.SetBlockInterval(blockInterval)
		go asset.MakeBlocks(bg, blockInterval)
	}
	http.Handle("/", chainhttp.ContextHandler{Context: bg, Handler: h})
	http.HandleFunc("/health", func(http.ResponseWriter, *http.Request) {})

	secureheader.DefaultConfig.PermitClearLoopback = true

	if *tlsCrt != "" {
		err = chainhttp.ListenAndServeTLS(*listenAddr, *tlsCrt, *tlsKey, secureheader.DefaultConfig)
	} else {
		err = http.ListenAndServe(*listenAddr, secureheader.DefaultConfig)
	}
	if err != nil {
		log.Fatalln("ListenAndServe:", err)
	}
}

func logWriter() io.Writer {
	dropmsg := []byte("\nlog data dropped\n")
	rotation := &errlog{w: rotation.Create(logFile, *logSize, *logCount)}
	splunk := &errlog{w: splunk.New(splunkAddr, dropmsg)}

	switch {
	case logFile != "" && splunkAddr != "":
		return io.MultiWriter(rotation, splunk)
	case logFile != "" && splunkAddr == "":
		return rotation
	case logFile == "" && splunkAddr != "":
		return splunk
	}
	return os.Stdout
}

type errlog struct {
	w io.Writer
	t time.Time // protected by chain/log mutex
}

func (w *errlog) Write(p []byte) (int, error) {
	// We don't want to ruin our performance
	// when there's a persistent error
	// writing to a log sink.
	// Print to stderr at most once per minute.
	_, err := w.w.Write(p)
	if err != nil && time.Since(w.t) > time.Minute {
		log.Println("chain/log:", err)
		w.t = time.Now()
	}
	return len(p), nil // report success for the MultiWriter
}
