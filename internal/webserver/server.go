package webserver

import (
	"fmt"
	"net/http"
	"path"
	"time"

	"github.com/go-shiori/shiori/internal/database"
	"github.com/julienschmidt/httprouter"
	cch "github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

// Config is parameter that used for starting web server
type Config struct {
	DB            database.DB
	DataDir       string
	ServerAddress string
	ServerPort    int
	RootPath      string
	Log           bool
}

// ErrorResponse defines a single HTTP error response.
type ErrorResponse struct {
	Code        int
	Body        string
	contentType string
	errorText   string
	Log         bool
}

func (e *ErrorResponse) Error() string {
	return e.errorText
}

func (e *ErrorResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e.contentType != "" {
		w.Header().Set("Content-Type", e.contentType)
	}
	body := e.Body
	if e.Code != 0 {
		w.WriteHeader(e.Code)
	}
	written := 0
	if len(body) > 0 {
		written, _ = w.Write([]byte(body))
	}
	if e.Log {
		Logger(r, e.Code, written)
	}
}

// responseData will hold response details that we are interested in for logging
type responseData struct {
	status int
	size   int
}

// Wrapper around http.ResponseWriter to be able to catch calls to Write*()
type loggingResponseWriter struct {
	http.ResponseWriter
	responseData *responseData
}

// Collect response size for each Write(). Also behave as the internal
// http.ResponseWriter by implicitely setting the status code to 200 at the
// first write.
func (r *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b) // write response using original http.ResponseWriter
	r.responseData.size += size            // capture size
	// Documented implicit WriteHeader(http.StatusOK) with first call to Write
	if r.responseData.status == 0 {
		r.responseData.status = http.StatusOK
	}
	return size, err
}

// Capture calls to WriteHeader, might be called on errors.
func (r *loggingResponseWriter) WriteHeader(statusCode int) {
	r.ResponseWriter.WriteHeader(statusCode) // write status code using original http.ResponseWriter
	r.responseData.status = statusCode       // capture status code
}

// Logger Log through logrus, 200 will log as info, anything else as an error.
func Logger(r *http.Request, statusCode int, size int) {
	if statusCode == http.StatusOK {
		logrus.WithFields(logrus.Fields{
			"proto":  r.Proto,
			"remote": GetUserRealIP(r),
			"reqlen": r.ContentLength,
			"size":   size,
			"status": statusCode,
		}).Info(r.Method, " ", r.RequestURI)
	} else {
		logrus.WithFields(logrus.Fields{
			"proto":  r.Proto,
			"remote": GetUserRealIP(r),
			"reqlen": r.ContentLength,
			"size":   size,
			"status": statusCode,
		}).Warn(r.Method, " ", r.RequestURI)
	}
}

func GetLegacyHandler(cfg Config) *Handler {
	return &Handler{
		DB:           cfg.DB,
		DataDir:      cfg.DataDir,
		UserCache:    cch.New(time.Hour, 10*time.Minute),
		SessionCache: cch.New(time.Hour, 10*time.Minute),
		ArchiveCache: cch.New(time.Minute, 5*time.Minute),
		RootPath:     cfg.RootPath,
		Log:          cfg.Log,
	}
}

// ServeApp serves web interface in specified port
func ServeApp(cfg Config) error {
	// Create handler
	hdl := GetLegacyHandler(cfg)

	hdl.PrepareSessionCache()
	hdl.prepareArchiveCache()

	err := hdl.PrepareTemplates()
	if err != nil {
		return fmt.Errorf("failed to prepare templates: %v", err)
	}

	// Prepare errors
	var (
		ErrorNotAllowed = &ErrorResponse{
			http.StatusMethodNotAllowed,
			"Method is not allowed",
			"text/plain; charset=UTF-8",
			"MethodNotAllowedError",
			cfg.Log,
		}
		ErrorNotFound = &ErrorResponse{
			http.StatusNotFound,
			"Resource Not Found",
			"text/plain; charset=UTF-8",
			"NotFoundError",
			cfg.Log,
		}
	)

	// Create router and register error handlers
	router := httprouter.New()
	router.NotFound = ErrorNotFound
	router.MethodNotAllowed = ErrorNotAllowed

	// withLogging will inject our own (compatible) http.ResponseWriter in order
	// to collect details about the answer, i.e. the status code and the size of
	// data in the response. Once done, these are passed further for logging, if
	// relevant.
	withLogging := func(req func(http.ResponseWriter, *http.Request, httprouter.Params)) func(http.ResponseWriter, *http.Request, httprouter.Params) {
		return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
			d := &responseData{
				status: 0,
				size:   0,
			}
			lrw := loggingResponseWriter{
				ResponseWriter: w,
				responseData:   d,
			}
			req(&lrw, r, ps)
			if hdl.Log {
				Logger(r, d.status, d.size)
			}
		}
	}

	// jp here means "join path", as in "join route with root path"
	jp := func(route string) string {
		return path.Join(cfg.RootPath, route)
	}

	router.GET(jp("/js/*filepath"), withLogging(hdl.serveJsFile))
	router.GET(jp("/res/*filepath"), withLogging(hdl.serveFile))
	router.GET(jp("/css/*filepath"), withLogging(hdl.serveFile))
	router.GET(jp("/fonts/*filepath"), withLogging(hdl.serveFile))

	router.GET(cfg.RootPath, withLogging(hdl.serveIndexPage))
	router.GET(jp("/login"), withLogging(hdl.serveLoginPage))
	router.GET(jp("/bookmark/:id/thumb"), withLogging(hdl.ServeThumbnailImage))
	router.GET(jp("/bookmark/:id/content"), withLogging(hdl.ServeBookmarkContent))
	router.GET(jp("/bookmark/:id/ebook"), withLogging(hdl.ServeBookmarkEbook))
	router.GET(jp("/bookmark/:id/archive/*filepath"), withLogging(hdl.ServeBookmarkArchive))

	router.POST(jp("/api/login"), withLogging(hdl.apiLogin))
	router.POST(jp("/api/logout"), withLogging(hdl.ApiLogout))
	router.GET(jp("/api/bookmarks"), withLogging(hdl.ApiGetBookmarks))
	router.GET(jp("/api/tags"), withLogging(hdl.ApiGetTags))
	router.PUT(jp("/api/tag"), withLogging(hdl.ApiRenameTag))
	router.POST(jp("/api/bookmarks"), withLogging(hdl.ApiInsertBookmark))
	router.DELETE(jp("/api/bookmarks"), withLogging(hdl.ApiDeleteBookmark))
	router.PUT(jp("/api/bookmarks"), withLogging(hdl.ApiUpdateBookmark))
	router.PUT(jp("/api/cache"), withLogging(hdl.ApiUpdateCache))
	router.PUT(jp("/api/ebook"), withLogging(hdl.ApiDownloadEbook))
	router.PUT(jp("/api/bookmarks/tags"), withLogging(hdl.ApiUpdateBookmarkTags))
	router.POST(jp("/api/bookmarks/ext"), withLogging(hdl.ApiInsertViaExtension))
	router.DELETE(jp("/api/bookmarks/ext"), withLogging(hdl.ApiDeleteViaExtension))

	router.GET(jp("/api/accounts"), withLogging(hdl.ApiGetAccounts))
	router.PUT(jp("/api/accounts"), withLogging(hdl.ApiUpdateAccount))
	router.POST(jp("/api/accounts"), withLogging(hdl.ApiInsertAccount))
	router.DELETE(jp("/api/accounts"), withLogging(hdl.ApiDeleteAccount))

	// Route for panic, keep logging anyhow
	router.PanicHandler = func(w http.ResponseWriter, r *http.Request, arg interface{}) {
		d := &responseData{
			status: 0,
			size:   0,
		}
		lrw := loggingResponseWriter{
			ResponseWriter: w,
			responseData:   d,
		}
		http.Error(&lrw, fmt.Sprint(arg), 500)
		if hdl.Log {
			Logger(r, d.status, d.size)
		}
	}

	// Create server
	url := fmt.Sprintf("%s:%d", cfg.ServerAddress, cfg.ServerPort)
	svr := &http.Server{
		Addr:         url,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: time.Minute,
	}

	// Serve app
	logrus.Infoln("Serve shiori in", url, cfg.RootPath)
	return svr.ListenAndServe()
}
