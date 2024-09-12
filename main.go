package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

var db *sql.DB // Global variable to store the database connection

// Main function starts the HTTP server and handles graceful shutdown
func main() {

	var err error
	// Open connection to PostgreSQL database using the constructed connection string
	db, err = sql.Open("postgres", "user=postgres dbname=test sslmode=disable")
	if err != nil {
		log.Fatal("Failed to connect to the database: ", err)
	}
	defer db.Close() // Ensure database connection is properly closed

	// Set up database connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Create an HTTP server instance
	srv := &http.Server{
		Addr: ":8080",
	}

	http.HandleFunc("/users", getUsers)    // Endpoint to retrieve users from the database
	http.HandleFunc("/create", createUser) // Endpoint to create a new user

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Println("Server is running on port 8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down server...")

	// Use a context with a 5-second timeout to gracefully shut down the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server Shutdown Failed:%+v", err)
	}
	log.Println("Server gracefully stopped")
}

// getUsers handles HTTP requests to retrieve users from the database
func getUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, name FROM users")
	if err != nil {
		sendErrorResponse(w, "Failed to query users", http.StatusInternalServerError)
		log.Println("Failed to query users: ", err)
		return
	}
	defer rows.Close()

	var users []map[string]interface{}
	for rows.Next() {
		var id int
		var name string
		err = rows.Scan(&id, &name)
		if err != nil {
			sendErrorResponse(w, "Failed to scan user", http.StatusInternalServerError)
			log.Println("Failed to scan user: ", err)
			return
		}
		users = append(users, map[string]interface{}{
			"id":   id,
			"name": name,
		})
	}

	if err = rows.Err(); err != nil {
		sendErrorResponse(w, "Error iterating users", http.StatusInternalServerError)
		log.Println("Error iterating users: ", err)
		return
	}

	sendSuccessResponse(w, map[string]interface{}{
		"users":  users,
		"status": http.StatusOK,
	}, http.StatusOK)
}

// createUser handles HTTP POST requests to add a new user to the database
func createUser(w http.ResponseWriter, r *http.Request) {
	// Convert createUser API from GET to POST to follow RESTful principles
	if r.Method != http.MethodPost {
		sendErrorResponse(w, "Invalid request method. Only POST is allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		sendErrorResponse(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	username := r.FormValue("name")
	if username == "" {
		sendErrorResponse(w, "Username is required", http.StatusBadRequest)
		return
	}

	// Validate the username to be between 3 and 20 characters
	if len(username) < 3 || len(username) > 20 {
		sendErrorResponse(w, "Username must be between 3 and 20 characters", http.StatusBadRequest)
		return
	}

	// Check if the username already exists before inserting to prevent duplicates
	exists, err := checkUsernameExists(username)
	if err != nil {
		sendErrorResponse(w, "Error checking username", http.StatusInternalServerError)
		log.Println("Error checking username:", err)
		return
	}
	if exists {
		// Return a 409 Conflict status if the username already exists
		sendErrorResponse(w, "Username already exists", http.StatusConflict)
		return
	}

	// Insert the new user into the database using parameterized queries to prevent SQL injection
	_, err = db.Exec("INSERT INTO users (name) VALUES ($1)", username)
	if err != nil {
		sendErrorResponse(w, "Failed to create user", http.StatusInternalServerError)
		log.Println("Failed to create user: ", err)
		return
	}

	sendSuccessResponse(w, map[string]interface{}{
		"message": fmt.Sprintf("User %s created successfully", username),
		"status":  http.StatusCreated,
	}, http.StatusCreated)
}

// checkUsernameExists checks if a given username already exists in the database
func checkUsernameExists(username string) (bool, error) {
	var exists bool
	// Check if the username already exists in the database
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE name=$1)", username).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// sendErrorResponse is a helper function to send a JSON error response
func sendErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":  message,
		"status": statusCode,
	})
}

// sendSuccessResponse is a helper function to send a JSON success response
func sendSuccessResponse(w http.ResponseWriter, data map[string]interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
