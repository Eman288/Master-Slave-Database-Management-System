package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

//#region Types and Globals

// QueryMessage represents the structure of messages between master and slave
type QueryMessage struct {
	Type    string `json:"type"`    // "query", "response", "table_create", etc.
	Query   string `json:"query"`   // The SQL query
	Success bool   `json:"success"` // Indicates if the query was successful
	Message string `json:"message"` // Additional information message
}

var (
	stopReadingFromMaster = false
	mutex                 sync.Mutex
)

//#endregion

func main() {
	//#region Setup logging and welcome

	logFile, err := os.OpenFile("slave.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal("Log file error:", err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)
	log.Println("Slave system started at", time.Now())

	fmt.Println("===================================")
	fmt.Println("Welcome to the Slave Database System")
	fmt.Println("===================================")

	//#endregion

	//#region Database configuration and connection

	dbUser, dbPass, dbDatabase := promptDBCredentials()
	db, err := connectToMySQL(dbUser, dbPass)
	if err != nil {
		log.Fatal(err)
		fmt.Println("Failed to connect to MySQL server:", err)
		return
	}
	defer db.Close()

	if err := prepareDatabase(db, dbDatabase); err != nil {
		log.Fatal(err)
		fmt.Println("Failed to prepare database:", err)
		return
	}

	//#endregion

	//#region Connect to master server

	masterAddr := promptMasterAddress()
	conn, err := connectToMaster(masterAddr)
	if err != nil {
		log.Fatal("Failed to connect to master:", err)
		fmt.Println("Failed to connect to master. Please check if master is running and try again.")
		return
	}
	defer conn.Close()

	log.Println("Connected to master at", time.Now())
	fmt.Println("Connected to master successfully.")

	handleInitialMasterMessage(conn)

	//#endregion

	//#region Start goroutines for master and slave query handling

	done := make(chan bool)
	go listenForQueries(conn, db, done)
	handleSlaveQueries(conn, db, bufio.NewReader(os.Stdin), done)

	//#endregion
}
//#region Database setup helpers

func promptDBCredentials() (user, pass, database string) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("User Name: ")
	user, _ = reader.ReadString('\n')
	user = strings.TrimSpace(user)

	fmt.Print("User Password: ")
	pass, _ = reader.ReadString('\n')
	pass = strings.TrimSpace(pass)

	fmt.Print("Database Name: ")
	database, _ = reader.ReadString('\n')
	database = strings.TrimSpace(database)

	return
}

func connectToMySQL(user, pass string) (*sql.DB, error) {
	connStr := fmt.Sprintf("%s:%s@tcp(localhost:3306)/", user, pass)
	db, err := sql.Open("mysql", connStr)
	if err != nil {
		return nil, fmt.Errorf("database connection error: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("database ping failed: %w", err)
	}
	fmt.Println("Connected to MySQL server successfully.")
	return db, nil
}

func prepareDatabase(db *sql.DB, database string) error {
	if _, err := db.Exec("CREATE DATABASE IF NOT EXISTS " + database); err != nil {
		return fmt.Errorf("error creating database: %w", err)
	}
	log.Println("Checked/Created database", database)
	fmt.Println("Database ready successfully.")

	if _, err := db.Exec("USE " + database); err != nil {
		return fmt.Errorf("db select error: %w", err)
	}
	return nil
}

//#endregion

//#region Master connection helpers

func promptMasterAddress() string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter Master IP address (default: localhost): ")
	masterIP, _ := reader.ReadString('\n')
	masterIP = strings.TrimSpace(masterIP)
	if masterIP == "" {
		masterIP = "localhost"
	}
	return masterIP + ":9000"
}

func connectToMaster(addr string) (net.Conn, error) {
	var conn net.Conn
	var err error

	maxRetries := 5
	retryDelay := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		conn, err = net.Dial("tcp", addr)
		if err == nil {
			break
		}

		log.Printf("Connection attempt %d failed: %v", i+1, err)
		fmt.Printf("Connection attempt %d failed: %v. Retrying in %v...\n", i+1, err, retryDelay)

		if i < maxRetries-1 {
			time.Sleep(retryDelay)
		}
	}
	return conn, err
}

func handleInitialMasterMessage(conn net.Conn) {
	reader := bufio.NewReader(conn)
	initialMsg, err := reader.ReadBytes('\n')
	if err != nil {
		log.Println("Error reading initial response from master:", err)
		fmt.Println("Connection to master was established but no confirmation received.")
		return
	}

	var responseMsg QueryMessage
	if err := json.Unmarshal(initialMsg, &responseMsg); err != nil {
		log.Println("Error parsing initial response:", err)
		return
	}

	fmt.Println("Master says:", responseMsg.Message)
}

//#endregion

//#region Slave CLI query handler

func handleSlaveQueries(conn net.Conn, db *sql.DB, reader *bufio.Reader, done chan bool) {
	for {
		fmt.Println("\n===================================")
		fmt.Println("Slave SQL Query Interface")
		fmt.Println("===================================")
		fmt.Println("Type your SQL query (INSERT, SELECT, UPDATE, DELETE)")
		fmt.Println("Note: CREATE and DROP operations are not allowed")
		fmt.Println("Or type 'exit' to quit.")
		fmt.Print("> ")

		query, _ := reader.ReadString('\n')
		query = strings.TrimSpace(query)

		if strings.ToLower(query) == "exit" {
			fmt.Println("Exiting slave client.")
			log.Println("Slave system exited normally")
			done <- true
			break
		}

		if query == "" {
			fmt.Println("Empty query, please type something.")
			continue
		}

		if !strings.HasSuffix(query, ";") {
			query += ";"
		}

		if strings.HasPrefix(strings.ToLower(query), "create") || strings.HasPrefix(strings.ToLower(query), "drop") {
			fmt.Println("Error: CREATE and DROP operations are not allowed from slave")
			continue
		}

		setStopReading(true)
		executeLocalQuery(db, query)
		sendQueryToMaster(conn, query)
		setStopReading(false)
	}
}

func setStopReading(val bool) {
	mutex.Lock()
	defer mutex.Unlock()
	stopReadingFromMaster = val
}

func executeLocalQuery(db *sql.DB, query string) {
	log.Println("Executing query locally first:", query)
	lowerQuery := strings.ToLower(query)

	if strings.HasPrefix(lowerQuery, "select") {
		rows, err := db.Query(query)
		if err != nil {
			log.Println("Local query error:", err)
			fmt.Println("Error executing query locally:", err)
			return
		}
		defer rows.Close()

		printQueryResults(rows, "Local Query Result")

	} else {
		result, err := db.Exec(query)
		if err != nil {
			log.Println("Local execution error:", err)
			fmt.Println("Error executing query locally:", err)
			return
		}

		rowsAffected, _ := result.RowsAffected()
		lastInsertID, _ := result.LastInsertId()
		log.Printf("Executed query locally. Rows affected: %d, LastID: %d\n", rowsAffected, lastInsertID)
		fmt.Printf("Query executed locally. Rows affected: %d\n", rowsAffected)
		if lastInsertID > 0 {
			fmt.Printf("Last insert ID: %d\n", lastInsertID)
		}
	}
}

func sendQueryToMaster(conn net.Conn, query string) {
	log.Println("Sending query to master:", query)

	message := QueryMessage{
		Type:  "query",
		Query: query,
	}
	messageJSON, err := json.Marshal(message)
	if err != nil {
		log.Println("Error creating JSON message:", err)
		fmt.Println("Error creating JSON message:", err)
		return
	}
	messageJSON = append(messageJSON, '\n')

	if _, err = conn.Write(messageJSON); err != nil {
		log.Println("Error sending query to master:", err)
		fmt.Println("Error sending query to master:", err)
		return
	}

	waitForMasterResponse(conn)
}

func waitForMasterResponse(conn net.Conn) {
	responseChannel := make(chan []byte)
	errorChannel := make(chan error)

	go func() {
		response, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			errorChannel <- err
			return
		}
		responseChannel <- response
	}()

	select {
	case response := <-responseChannel:
		var responseMsg QueryMessage
		if err := json.Unmarshal(response, &responseMsg); err != nil {
			log.Println("Error parsing response:", err)
			fmt.Println("Error parsing response:", err)
		} else {
			if responseMsg.Success {
				fmt.Println("\n--- Master Response ---")
				fmt.Println("Status: Success")
				if responseMsg.Query != "" {
					fmt.Println(responseMsg.Query)
				}
				fmt.Println(responseMsg.Message)
			} else {
				fmt.Println("\n--- Master Error ---")
				fmt.Println(responseMsg.Message)
			}
			log.Println("Received response from master:", responseMsg.Message)
		}

	case err := <-errorChannel:
		log.Println("Error reading response from master:", err)
		fmt.Println("Error reading response from master:", err)

	case <-time.After(5 * time.Second):
		log.Println("Timeout waiting for master response")
		fmt.Println("Timeout waiting for master response")
	}
}

//#endregion

//#region Listener for master queries

func listenForQueries(conn net.Conn, db *sql.DB, done chan bool) {
	reader := bufio.NewReader(conn)

	for {
		select {
		case <-done:
			return
		default:
		}

		if getStopReading() {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		message, err := reader.ReadBytes('\n')

		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Println("Connection to master closed or read error:", err)
			fmt.Println("\nLost connection to master:", err)
			return
		}
		conn.SetReadDeadline(time.Time{})

		var queryMsg QueryMessage
		if err := json.Unmarshal(message, &queryMsg); err != nil {
			log.Println("Error parsing message from master:", err)
			continue
		}

		if queryMsg.Type == "response" {
			log.Printf("Received response from master: %s\n", queryMsg.Message)
			fmt.Printf("\nReceived from master: %s\n", queryMsg.Message)
			continue
		}

		query := strings.TrimSpace(queryMsg.Query)
		if query == "" {
			continue
		}

		log.Printf("Received query from master: %s\n", query)
		fmt.Printf("\nReceived query from master: %s\n", query)

		if queryMsg.Type == "table_create" {
			log.Println("Creating table from master:", query)
			if _, err := db.Exec(query); err != nil {
				log.Println("Error creating table:", err)
				fmt.Println("Error creating table:", err)
			} else {
				log.Println("Table created successfully")
				fmt.Println("Table created successfully")
			}
			continue
		}

		executeMasterQuery(db, query)
	}
}
func getStopReading() bool {
	mutex.Lock()
	defer mutex.Unlock()
	return stopReadingFromMaster
}
func executeMasterQuery(db *sql.DB, query string) {
	if strings.HasPrefix(strings.ToLower(query), "select") {
		rows, err := db.Query(query)
		if err != nil {
			log.Println("Query error:", err)
			fmt.Println("Error executing master's query:", err)
			return
		}
		defer rows.Close()

		printQueryResults(rows, "Master Query Result")

	} else {
		result, err := db.Exec(query)
		if err != nil {
			log.Println("Execution error:", err)
			fmt.Println("Error executing master's query:", err)
			return
		}
		rowsAffected, _ := result.RowsAffected()
		log.Printf("Executed master's query successfully. Rows affected: %d\n", rowsAffected)
		fmt.Printf("Master query executed successfully. Rows affected: %d\n", rowsAffected)
	}
}

//#endregion

//#region Utility to print query results

func printQueryResults(rows *sql.Rows, header string) {
	fmt.Printf("\n--- %s ---\n", header)

	cols, _ := rows.Columns()
	values := make([]sql.RawBytes, len(cols))
	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	// Print column names
	for i, col := range cols {
		if i > 0 {
			fmt.Print("\t")
		}
		fmt.Print(col)
	}
	fmt.Println()

	// Print separator
	for i := 0; i < len(cols); i++ {
		if i > 0 {
			fmt.Print("\t")
		}
		fmt.Print("--------")
	}
	fmt.Println()

	rowCount := 0
	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			log.Println("Row scan error:", err)
			break
		}
		rowCount++

		for i, val := range values {
			if i > 0 {
				fmt.Print("\t")
			}
			if val == nil {
				fmt.Print("NULL")
			} else {
				fmt.Print(string(val))
			}
		}
		fmt.Println()
	}
	fmt.Printf("\n%d row(s) returned\n", rowCount)
}

//#endregion
