package api

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	APIRoute        = "/api/blackouts"
	StaticRoute     = "/"
	StaticDirectory = "./web/static"
	OutFilesPattern = "out/*.json"
)

func Serve(port string) {
	http.HandleFunc(APIRoute, handleBlackouts)

	fs := http.FileServer(http.Dir(StaticDirectory))
	http.Handle(StaticRoute, fs)

	log.Printf("Server starting on http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func handleBlackouts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var allNotices []interface{}
	files, err := filepath.Glob(OutFilesPattern)
	if err != nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var notices []interface{}
		if err := json.Unmarshal(data, &notices); err == nil {
			allNotices = append(allNotices, notices...)
		}
	}

	if allNotices == nil {
		allNotices = []interface{}{}
	}

	json.NewEncoder(w).Encode(allNotices)
}
