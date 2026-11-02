package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lcmps/retroxide/application/setup"
	"github.com/lcmps/retroxide/infrastructure/config"
)

func TestRunSetupSwitchesToApplicationAfterCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	configPath := filepath.Join(directory, "retroxide.env")
	t.Setenv(config.ConfigFileKey, configPath)
	t.Setenv("RETROXIDE_HOST", "127.0.0.1")
	t.Setenv("RETROXIDE_PORT", fmt.Sprint(port))
	t.Setenv("RETROXIDE_JWT_SECRET", "")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runSetup(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	baseURL := "http://" + net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	client := &http.Client{Timeout: 10 * time.Second}

	var status setup.Status
	deadline := time.Now().Add(2 * time.Second)
	for {
		response, err := client.Get(baseURL + "/api/v1/setup")
		if err == nil {
			err = json.NewDecoder(response.Body).Decode(&status)
			_ = response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("setup server did not start: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !status.Required || status.Defaults == nil {
		t.Fatalf("status = %+v, want required with defaults", status)
	}
	response, err := client.Get(baseURL + "/api/v1/rom")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("app route during setup = %d, want 503", response.StatusCode)
	}

	request := setup.Request{
		DBDriver: setup.DriverSQLite, SQLitePath: filepath.Join(directory, "app.sqlite3"),
		StorageRoot: filepath.Join(directory, "storage"),
		AdminEmail:  "admin@example.com", AdminPassword: "correct horse battery staple",
	}
	postJSON(t, client, baseURL+"/api/v1/setup", request, http.StatusNoContent)
	postJSON(t, client, baseURL+"/api/v1/session", map[string]string{
		"email": request.AdminEmail, "password": request.AdminPassword,
	}, http.StatusOK)

	response, err = client.Get(baseURL + "/api/v1/setup")
	if err != nil {
		t.Fatal(err)
	}
	status = setup.Status{}
	err = json.NewDecoder(response.Body).Decode(&status)
	_ = response.Body.Close()
	if err != nil || status.Required {
		t.Fatalf("status after setup = %+v, %v", status, err)
	}
	if _, err := config.LoadEnvironment(configPath, nil); err != nil {
		t.Fatalf("saved config does not load: %v", err)
	}

	client.CloseIdleConnections()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runSetup() = %v", err)
		}
	case <-time.After(shutdownTimeout + 2*time.Second):
		t.Fatal("runSetup() did not stop after cancellation")
	}
}

func postJSON(t *testing.T, client *http.Client, url string, payload any, wantStatus int) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		contents, _ := io.ReadAll(response.Body)
		t.Fatalf("POST %s = %d %s, want %d", url, response.StatusCode, contents, wantStatus)
	}
}
