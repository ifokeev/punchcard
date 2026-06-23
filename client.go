package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

func baseURL() string {
	if u := os.Getenv("PUNCH_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:8080"
}

func authReq(method, path string, body io.Reader, contentType string) (*http.Request, error) {
	req, err := http.NewRequest(method, baseURL()+path, body)
	if err != nil {
		return nil, err
	}
	if tok := os.Getenv("PUNCH_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

func doJSON(method, path string, payload any) (int, []byte, error) {
	var buf io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		buf = bytes.NewReader(b)
	}
	req, err := authReq(method, path, buf, "application/json")
	if err != nil {
		return 0, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, nil
}

func uploadFile(id, path string) (int, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", filepath.Base(path))
	io.Copy(fw, f)
	mw.Close()
	req, err := authReq("POST", "/api/tasks/"+id+"/artifacts", &buf, mw.FormDataContentType())
	if err != nil {
		return 0, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, nil
}

// doDelete sends a DELETE request and returns the status code and body.
func doDelete(path string) (int, []byte, error) {
	req, err := authReq("DELETE", path, nil, "")
	if err != nil {
		return 0, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, nil
}

// readStdin reads all of os.Stdin and returns it.
func readStdin() ([]byte, error) {
	return io.ReadAll(os.Stdin)
}

// urlEncode percent-encodes a query parameter value.
func urlEncode(s string) string {
	return url.QueryEscape(s)
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
