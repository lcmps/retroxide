package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"

	"github.com/gin-gonic/gin"
	"github.com/lcmps/retroxide/internal/features/auth"
	"github.com/lcmps/retroxide/internal/features/jobs"
	"github.com/lcmps/retroxide/internal/features/uploads"
	"github.com/lcmps/retroxide/internal/platform/config"
)

func TestBuildCreatesSQLiteSchemaAndHealthRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := config.Config{
		RuntimeEnv:              "test",
		Host:                    "127.0.0.1",
		Port:                    0,
		DBDriver:                "sqlite",
		SQLitePath:              filepath.Join(t.TempDir(), "retroxide.sqlite3"),
		MySQLURL:                "mysql://root:root@127.0.0.1:3306/retroxide",
		StorageRoot:             filepath.Join(t.TempDir(), "storage"),
		MaxUploadBytes:          1024,
		AllowedUploadMIMETypes:  []string{"application/octet-stream"},
		AllowedUploadExtensions: []string{"sfc"},
		JWTSecret:               strings.Repeat("s", 32),
		JWTAccessExpirySecs:     900,
		JWTRefreshExpirySecs:    604800,
		WorkerCount:             1,
		AdminEmail:              "admin@example.com",
		AdminPassword:           "correct horse battery staple",
	}
	app, err := Build(context.Background(), settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Error(err)
		}
	})
	workerContext, cancelWorker := context.WithCancel(context.Background())
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- jobs.Run(workerContext, settings.WorkerCount, app.jobQueue, app.identifier)
	}()
	defer func() {
		cancelWorker()
		if err := <-workerDone; err != nil {
			t.Errorf("worker shutdown: %v", err)
		}
	}()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("health = %d %q", response.Code, response.Body.String())
	}

	loginBody := strings.NewReader(`{"email":"admin@example.com","password":"correct horse battery staple"}`)
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/session", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	app.Router().ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login = %d %s", loginResponse.Code, loginResponse.Body.String())
	}
	var session auth.SessionResponse
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatalf("session response missing tokens: %+v", session)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/rom", nil)
	listRequest.Header.Set("Authorization", "Bearer "+session.AccessToken)
	listResponse := httptest.NewRecorder()
	app.Router().ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"item":[]`) {
		t.Fatalf("ROM list = %d %s", listResponse.Code, listResponse.Body.String())
	}

	var multipartBody bytes.Buffer
	multipartWriter := multipart.NewWriter(&multipartBody)
	fileWriter, err := multipartWriter.CreateFormFile("file", "Super Mario World (USA).sfc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileWriter.Write([]byte("rom-payload")); err != nil {
		t.Fatal(err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/v1/upload", &multipartBody)
	uploadRequest.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	uploadRequest.Header.Set("Authorization", "Bearer "+session.AccessToken)
	uploadResponse := httptest.NewRecorder()
	app.Router().ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusAccepted {
		t.Fatalf("upload = %d %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var accepted uploads.UploadAcceptedDTO
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/upload/"+accepted.UploadID, nil)
		statusRequest.Header.Set("Authorization", "Bearer "+session.AccessToken)
		statusResponse := httptest.NewRecorder()
		app.Router().ServeHTTP(statusResponse, statusRequest)
		if statusResponse.Code != http.StatusOK {
			t.Fatalf("upload status = %d %s", statusResponse.Code, statusResponse.Body.String())
		}
		if strings.Contains(statusResponse.Body.String(), `"status":"completed"`) {
			if !strings.Contains(statusResponse.Body.String(), `"rom_id":"rom_`) {
				t.Fatalf("completed upload lacks rom id: %s", statusResponse.Body.String())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("upload did not complete before timeout: %s", statusResponse.Body.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
	completedListRequest := httptest.NewRequest(http.MethodGet, "/api/v1/rom", nil)
	completedListRequest.Header.Set("Authorization", "Bearer "+session.AccessToken)
	completedListResponse := httptest.NewRecorder()
	app.Router().ServeHTTP(completedListResponse, completedListRequest)
	if completedListResponse.Code != http.StatusOK || !strings.Contains(completedListResponse.Body.String(), "Super Mario World") {
		t.Fatalf("identified ROM list = %d %s", completedListResponse.Code, completedListResponse.Body.String())
	}
}

func TestRunStopsServerAndWorkerOnContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	settings := config.Config{
		RuntimeEnv: "test", Host: "127.0.0.1", Port: uint16(port), DBDriver: "sqlite",
		SQLitePath: filepath.Join(t.TempDir(), "shutdown.sqlite3"), StorageRoot: filepath.Join(t.TempDir(), "storage"),
		MaxUploadBytes: 1024, AllowedUploadMIMETypes: []string{"application/octet-stream"},
		AllowedUploadExtensions: []string{"sfc"}, JWTSecret: strings.Repeat("s", 32),
		JWTAccessExpirySecs: 900, JWTRefreshExpirySecs: 604800, WorkerCount: 1,
	}
	app, err := Build(context.Background(), settings, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	for {
		response, requestErr := client.Get("http://" + net.JoinHostPort(settings.Host, fmt.Sprint(settings.Port)) + "/health")
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				cancel()
				t.Fatalf("health status = %d", response.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("server did not start: %v", requestErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() returned shutdown error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
}
