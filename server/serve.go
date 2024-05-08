package server

// Package to start the CWF server and handle all actions.

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"cwf/entities"

	"go.uber.org/zap"
)

var FILE_SUFFIX string = ".cwf"
var filesDir string

// Start the server and setup needed endpoits.
func StartServer(port int, argFilesDir string) {
	zap.L().Info("Welcome to CopyWithFriends -> cwf")
	filesDir = argFilesDir

	// Endpoints
	http.HandleFunc("/cwf/get", handleStdout)
	http.HandleFunc("/cwf/copy", handleStdin)
	http.HandleFunc("/cwf/delete", handleDelete)
	http.HandleFunc("/cwf/list", handleList)

	// Changing default errorHandler for unknown endpoints
	http.HandleFunc("/", errorHandler)

	// TODO: Make port either use global var or better via comline line or config file
	log.Fatal(http.ListenAndServe(":" + fmt.Sprint(port), nil))
}

// handleStdout is called on `GET` to return the saved content of a file.
func handleStdout(w http.ResponseWriter, r *http.Request) {
	zap.L().Info("Calling handleStdout")

	if !allowedEndpoint(r.URL, "get") {
		writeRes(w, http.StatusForbidden, "Invalid endpoint!")
		return
	}

	file := r.URL.Query().Get("file")
	if file == "" {
		zap.L().Info("No file name or path provided!")
		writeRes(w, http.StatusBadRequest, "No file name or path provided!")
		return
	}

	content, err := os.ReadFile(filesDir + file + FILE_SUFFIX)
	if err != nil {
		zap.L().Info("File not found! Filename: " + file)
		writeRes(w, http.StatusNotFound, "File not found!")
		return
	}

	w.Write(content)
}

// handleStdin is called on `POST` to handle file saves.
// It also is able to create a directory, if a full path is sent.
func handleStdin(w http.ResponseWriter, r *http.Request) {
	if !allowedEndpoint(r.URL, "copy") {
		writeRes(w, http.StatusForbidden, "Invalid endpoint!")
		return
	}

	var body entities.CWFBody_t
	err := json.NewDecoder(r.Body).Decode(&body)
	if err != nil {
		zap.L().Error("Failed to decode request body! Error: " + err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// TODO: Resolve path and compare with configured basedir
	// Example confiuration: /tmp/cwf/
	// body.File = ../test.cwf resolves to: `/tmp` -> not allowed (not in basedir)
	// body.File = ../var/ resolves to: `/var` -> also not allowed (not in basedir)
	if strings.Contains(body.File, "..") {
		zap.L().Error("File provided contains \"..\"")
		writeRes(w, http.StatusForbidden, "Not allowd!")
		return
	}

	if strings.Contains(body.File, "/") {
		dirs := strings.Split(body.File, "/")
		if len(dirs) > 2 {
			writeRes(w, http.StatusForbidden, "Not allowd! Directory depth must not exceed 2 levels")
			return
		}

		if _, err := os.Stat(filesDir + dirs[0]); os.IsNotExist(err) {
			err = os.Mkdir(filesDir + dirs[0], os.ModePerm)
			if err != nil {
				zap.L().Error("Error while creating new directory: " + err.Error())
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
	}

	err = os.WriteFile(filesDir + body.File + FILE_SUFFIX, []byte(body.Content), 0644)
	if err != nil {
		zap.L().Error("Error while creating/writing file! Error: " + err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeRes(w, http.StatusOK, "Saved to: " + body.File)
}

// handleDelete is called on `DELETE` to clean the directory or file.
// TODO: Dir support needs to be implemented.
func handleDelete(w http.ResponseWriter, r *http.Request) {
	if !allowedEndpoint(r.URL, "delete") {
		writeRes(w, http.StatusForbidden, "Invalid endpoint!")
		return
	}

	file := r.URL.Query().Get("file")
	if file == "" {
		zap.L().Warn("No file or path provided")
		writeRes(w, http.StatusBadRequest, "No file name or path provided!")
		return
	}

	err := os.Remove(filesDir + file + FILE_SUFFIX)
	if err != nil {
		zap.L().Warn("File not found")
		writeRes(w, http.StatusNotFound, "File not found!")
		return
	}

	writeRes(w, http.StatusOK, "Deleted file: " + file)
}

// Function to return all files/directories in given query parameter
// If no query parameter is provided we list files in root folder
func handleList(w http.ResponseWriter, r *http.Request) {
	// TODO: This has been now written 5 times we should use a wrapper for this call
	// INFO: My implemenation = not really helpfull
	if !allowedEndpoint(r.URL, "list") {
		writeRes(w, http.StatusForbidden, "Invalid endpoint!")
		return
	}

	targetDir := r.URL.Query().Get("dir")
	targetDir = filesDir + targetDir

	if strings.Contains(targetDir, "..") {
		zap.L().Warn("Client tried to call \"..\" Called by: " + r.RemoteAddr)
		writeRes(w, http.StatusForbidden, "Not allowed!")
		return
	}

	content, err := os.ReadDir(targetDir)
	if err != nil {
		zap.L().Warn(err.Error() + "Called By: " + r.RemoteAddr)
		writeRes(w, http.StatusNotFound, "No such directory!")
		return
	}

	sort.Slice(content, func(i int, j int) bool {
		fileI, _ := content[i].Info()
		fileJ, _ := content[j].Info()
		return fileI.ModTime().Before(fileJ.ModTime())
	})

	var response string
	for _, e := range content {
		if !e.IsDir() && !strings.HasSuffix(e.Name(), ".cwf") {
			continue
		}

		modificationTime, _ := e.Info()
		if e.Type().IsDir() {
			response += "Dir: \t" + e.Name() + "\t\t Modified: " + modificationTime.ModTime().Format("2006-01-02 15:04:05") + "\n"
			continue
		} else if e.Type().IsRegular() {
			response += "File: \t" + e.Name() + "\t\t Modified: " + modificationTime.ModTime().Format("2006-01-02 15:04:05") + "\n"
			continue
		}
	}

	writeRes(w, http.StatusOK, response)
}

func errorHandler(w http.ResponseWriter, r *http.Request) {
	zap.L().Warn("User called Endpoint " + r.URL.Path + " and is a bad boy")
	writeRes(w, http.StatusNotFound, "YOU ARE A BAD BOY, ONLY USE cwf client for making requests\n")
	// TODO: We should probabyl ban/block such ip addresses which try acces endpoints without the cwf client
}

// Respond the go way.
func writeRes(w http.ResponseWriter, statuscode int, content string) {
	w.WriteHeader(statuscode)
	w.Write([]byte(content))
}

// Check if endpoint is allowed for current action.
func allowedEndpoint(filepath *url.URL, endpoint string) bool {
	zap.L().Info("Called endpoint: " + filepath.Path + " Query: " + filepath.RawQuery)
	return path.Base(filepath.Path) == endpoint
}
