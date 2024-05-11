package server

// Package to start the CWF server and handle all actions.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"cwf/entities"
	"cwf/utilities"

	"go.uber.org/zap"
)

var FILE_SUFFIX string = ".cwf"
var filesDir string
var port int

// Init server
func initServer() bool {
	filesDir = utilities.GetFlagValue[string]("filesdir")
	port = utilities.GetFlagValue[int]("port")

	if _, err := os.Stat(filesDir); os.IsNotExist(err) {
		err := os.Mkdir(filesDir, 0777)
		if err != nil {
			zap.L().Error(err.Error())
			return false
		}
	}

	return true
}

// Start the server and setup needed endpoits.
func StartServer() {
	if !initServer() {
		return
	}

	zap.L().Info("Welcome to CopyWithFriends -> cwf")
	mux := http.NewServeMux()

	// Endpoints
	mux.HandleFunc("GET /cwf/content/{pathname...}", handleGetContent)
	mux.HandleFunc("POST /cwf/content/{pathname...}", handlePostContent)
	mux.HandleFunc("DELETE /cwf/delete/{pathname...}", handleDelete)
	mux.HandleFunc("GET /cwf/list/{pathname...}", handleList)

	// Changing default errorHandler for unknown endpoints
	// INFO: I don't think we need this anymore.
	// mux.HandleFunc("/", handleNotFound)

	zap.L().Info("Serving on PORT: " + strconv.Itoa(port))
	if !utilities.GetFlagValue[bool]("https") {
		log.Fatal(http.ListenAndServe(":" + fmt.Sprint(port), mux))
	} else {
		log.Fatal(http.ListenAndServeTLS(":" + fmt.Sprint(port), "certpath", "keypath", mux))
	}
}

// handleStdout is called on `GET` to return the saved content of a file.
func handleGetContent(writer http.ResponseWriter, req *http.Request) {
	zap.L().Info("Calling handleGetContent")

	if !allowedEndpoint(req.URL, "get") {
		writeRes(writer, http.StatusForbidden, "Invalid endpoint!")
		return
	}

	pathname := req.PathValue("pathname")
	if pathname == "" {
		zap.L().Info("No file name or path provided!")
		writeRes(writer, http.StatusBadRequest, "No file name or path provided!")
		return
	}

	content, err := os.ReadFile(filesDir + pathname + FILE_SUFFIX)
	if err != nil {
		zap.L().Info("File not found! Filename: " + pathname)
		writeRes(writer, http.StatusNotFound, "File not found!")
		return
	}

	writer.Write(content)
}

// handleStdin is called on `POST` to handle file saves.
// It also is able to create a directory, if a full path is sent.
func handlePostContent(writer http.ResponseWriter, req *http.Request) {
	if !allowedEndpoint(req.URL, "copy") {
		writeRes(writer, http.StatusForbidden, "Invalid endpoint!")
		return
	}

	pathname := req.PathValue("pathname")
	if pathname == "" {
		zap.L().Info("No file name or path provided!")
		writeRes(writer, http.StatusBadRequest, "No file name or path provided!")
		return
	}

	var body entities.CWFBody_t
	err := json.NewDecoder(req.Body).Decode(&body)
	if err != nil {
		zap.L().Error("Failed to decode request body! Error: " + err.Error())
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}

	// FIXME: We should definitely resolve paths and check if resolved path is in basedir.
	// TODO: Resolve path and compare with configured basedir
	// Example confiuration: /tmp/cwf/
	// body.File = ../test.cwf resolves to: `/tmp` -> not allowed (not in basedir)
	// body.File = ../var/ resolves to: `/var` -> also not allowed (not in basedir)
	if strings.Contains(pathname, "/") {
		dirs := strings.Split(pathname, "/")
		if len(dirs) > 2 {
			writeRes(writer, http.StatusForbidden, "Not allowd! Directory depth must not exceed 2 levels")
			return
		}

		if !isValidPath(dirs[0]) {
			zap.L().Warn("Client tried to call something bad Called by: " + req.RemoteAddr)
			writeRes(writer, http.StatusForbidden, "Not allowed!")
			return
		}

		if !isValidPath(dirs[1]) {
			zap.L().Warn("Client tried to call something bad Called by: " + req.RemoteAddr)
			writeRes(writer, http.StatusForbidden, "Not allowed!")
			return
		}

		if _, err := os.Stat(filesDir + dirs[0]); os.IsNotExist(err) {
			err = os.Mkdir(filesDir + dirs[0], os.ModePerm)
			if err != nil {
				zap.L().Error("Error while creating new directory: " + err.Error())
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
		}
	}

	err = os.WriteFile(filesDir + pathname + FILE_SUFFIX, []byte(body.Content), 0644)
	if err != nil {
		zap.L().Error("Error while creating/writing file! Error: " + err.Error())
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}

	writeRes(writer, http.StatusOK, "Saved to: " + pathname)
}

// handleDelete is called on `DELETE` to clean the directory or file.
func handleDelete(writer http.ResponseWriter, req *http.Request) {
	if !allowedEndpoint(req.URL, "delete") {
		writeRes(writer, http.StatusForbidden, "Invalid endpoint!")
		return
	}

	target := req.PathValue("pathname")
	if target == "" {
		zap.L().Warn("No file or path provided")
		writeRes(writer, http.StatusBadRequest, "No file name or path provided!")
		return
	}

	// FIXME: We should definitely resolve paths and check if resolved path is in basedir.
	if !isValidPath(target) {
		zap.L().Warn("Client tried to call something bad Called by: " + req.RemoteAddr)
		writeRes(writer, http.StatusForbidden, "Not allowed!")
		return
	}

	var err error
	var msg string
	if strings.HasSuffix(target, "/") {
		err = os.RemoveAll(filesDir + target)
		msg = "Deleted directory: " + strings.TrimSuffix(target, "/")
	} else {
		err = os.Remove(filesDir + target + FILE_SUFFIX)
		msg = "Deleted file: " + target
	}

	if err != nil {
		zap.L().Error(err.Error())
		writeRes(writer, http.StatusNotFound, "File or directory not found!")
		return
	}

	writeRes(writer, http.StatusOK, msg)
}

// Function to return all files/directories in given query parameter
// If no query parameter is provided we list files in root folder
func handleList(writer http.ResponseWriter, req *http.Request) {
	if !allowedEndpoint(req.URL, "list") {
		writeRes(writer, http.StatusForbidden, "Invalid endpoint!")
		return
	}

	targetDir := req.PathValue("pathname")
	if !isValidPath(targetDir) {
		zap.L().Warn("Client tried to call something bad Called by: " + req.RemoteAddr)
		writeRes(writer, http.StatusForbidden, "Not allowed!")
		return
	}

	targetDir = filesDir + targetDir
	content, err := os.ReadDir(targetDir)
	if err != nil {
		zap.L().Warn(err.Error() + "Called By: " + req.RemoteAddr)
		writeRes(writer, http.StatusNotFound, "No such directory!")
		return
	}

	sort.Slice(content, func(i int, j int) bool {
		fileI, _ := content[i].Info()
		fileJ, _ := content[j].Info()
		return fileI.ModTime().Before(fileJ.ModTime())
	})

	maxNameLen := 0
	for _, entry := range content {
		if len(entry.Name()) > maxNameLen {
			maxNameLen = len(entry.Name())
		}
	}

	maxNameLen += 1

	var response string
	response += fmt.Sprintf("Type    Name" + fmt.Sprintf("%*s", maxNameLen-4, "") + "Modified\n")

	var entryType string

	for _, e := range content {
		if !e.IsDir() && !strings.HasSuffix(e.Name(), ".cwf") {
			continue
		}

		modificationTime, _ := e.Info()
		if e.Type().IsDir() {
			entryType = "Dir"
		} else if e.Type().IsRegular() {
			entryType = "File"
		}

		response += fmt.Sprintf("%-7s%-*s%s\n", entryType, maxNameLen, e.Name(), modificationTime.ModTime().Format("2006-01-02 15:04:05"))
	}

	writeRes(writer, http.StatusOK, response)
}

// Default handler for 404 pages
func handleNotFound(w http.ResponseWriter, r *http.Request) {
	zap.L().Warn("User called Endpoint " + r.URL.Path + " and is a bad boy")
	writeRes(w, http.StatusNotFound, "YOU ARE A BAD BOY, ONLY USE cwf client for making requests\n")
	// TODO: We should probabyl ban/block such ip addresses which try acces endpoints without the cwf client
}

// Respond the go way.
func writeRes(w http.ResponseWriter, statuscode int, content string) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(statuscode)
	w.Write([]byte(content))
}

// Check if endpoint is allowed for current action.
func allowedEndpoint(filepath *url.URL, endpoint string) bool {
	zap.L().Info("Called endpoint: " + filepath.Path + " Query: " + filepath.RawQuery)
	return path.Base(filepath.Path) == endpoint
}

// Check if filename is valid
func isValidPath(filename string) bool {
	return !strings.ContainsAny(filename, "\\\\:*?\\<>|..")
}

// Check if filename is valid
// func isValidPath(filename string) bool {
// }
