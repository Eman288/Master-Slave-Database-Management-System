package main
//imports
import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "github.com/go-sql-driver/mysql"
)
//region  variables_initialization 
var dbUser, dbPass, dbDatabase, dbTable, dbColumn string
var db *sql.DB
var reader = bufio.NewReader(os.Stdin)
var clients = make(map[net.Conn]bool)
var clientsMutex sync.Mutex
//endregion

// var templates = template.Must(template.ParseGlob("templates/*.html"))
var templates *template.Template
var once sync.Once // to ensure slave server and DB setup start only once

// QueryMessage represents the structure of messages between master and slave
type QueryMessage struct {
	Type    string `json:"type"`    // "query", "response", "table_create", etc.
	Query   string `json:"query"`   // The SQL query
	Success bool   `json:"success"` // Indicates if the query was successful
	Message string `json:"message"` // Additional information message
}
func init() {
	tmplFuncs := template.FuncMap{
		"hasPrefix": strings.HasPrefix,
		// add more helpers here if needed
	}
	t, err := template.New("").Funcs(tmplFuncs).ParseGlob("templates/*.html")
	if err != nil {
		log.Fatalf("Template parsing failed: %v", err)
	}
	templates = t
}
//region login handler
func login(w http.ResponseWriter, r *http.Request) {
	if r.Method!=http.MethodPost{
		http.Error(w,"Method not allowed",http.StatusMethodNotAllowed)
		return
	}
		dbUser = r.FormValue("user")
		dbPass = r.FormValue("pass")
		sqlCon := dbUser + ":" + dbPass + "@tcp(localhost:3306)/"
		var err error
		db, err = sql.Open("mysql", sqlCon)
		if err != nil {
			log.Println("Connection error:", err)
			http.Error(w,"Database connection error",500)
			return
		}
		err = db.Ping()//check connection if it is still alive 
		if err != nil {
			log.Println("Database ping failed:", err)
			http.Error(w, "Database login failed", 401)
			return
		}

		log.Println("Login successful")
		go once.Do(startConsoleAndTCP) // (go) it runs in a background goroutine, so it doesn't block other code. (once.Do) ensures that startConsoleAndTCP is only ever executed one time — no matter how many users log in or how many times it's called.
	
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}
//endregion

type HomePageData struct {Databases []string  ;DbDatabase string ;Tables []string}
type TableContent struct {Columns []string ;ColTypes []string ; Rows [][]interface{} ;Table string ;RowNum  int ;ColNum int}
//region AllHandlers for DB
//region Home handler 
func home(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
		//step 1 => get all databases
		query := "SHOW DATABASES"
		rows, err := db.Query(query)
		if err != nil {
			http.Error(w, "Database query failed", http.StatusInternalServerError)
			log.Println("Query error:", err);return
		}
		defer rows.Close()
		var dbNames []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				log.Println("Row scan error:", err);continue
			}
			dbNames = append(dbNames,name)
		}
		if err := rows.Err(); err != nil {
			log.Println("Rows iteration error:", err)
			http.Error(w, "Row iteration failed", http.StatusInternalServerError)
			return
		}
		// step 2 => get tables from that database,If dbDatabase is set,
		var ( dbName  string ;  tbNames []string ;)
	if dbDatabase != "" {
		tbQuery := "SHOW TABLES FROM " + dbDatabase
		tbRows, err := db.Query(tbQuery)
		if err != nil {
			log.Println("Failed to query tables:", err)
		} else {
			defer tbRows.Close()
			for tbRows.Next() {
				var table string
				if err := tbRows.Scan(&table); err != nil {
					log.Println("Table scan error:", err);continue
				}
				tbNames = append(tbNames, table)
			}
			if err := tbRows.Err(); err != nil {
				log.Println("Tables iteration error:", err)
			}
		}
		dbName = dbDatabase
	} else {
		dbName = "Not Selected"
	}

	//step 3 => build and render template
		homeData := HomePageData{Databases:  dbNames , DbDatabase: dbName , Tables:  tbNames,
		}

		if err := templates.ExecuteTemplate(w, "home.html", homeData); err != nil {
			http.Error(w, "Template rendering failed: "+err.Error(), http.StatusInternalServerError)
			log.Println("Template error:", err)
		}
}
//endregion

//*************************** DataBase Level **********************************

//region create DB Handler
func createDb(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	dbDatabase = strings.TrimSpace(r.FormValue("dbName"))
	if dbDatabase == "" {
		http.Error(w, "Missing database name", http.StatusBadRequest)
		return
	}
	log.Println("Received dbName:", dbDatabase)

	if db == nil {
		log.Println("DB connection is nil")
		http.Error(w, "No DB connection", http.StatusInternalServerError)
		return
	}

	safeDbName := strings.ReplaceAll(dbDatabase, "`", "")
	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", safeDbName)

	if _, err := db.Exec(query); err != nil {
		log.Printf("Error creating database %s: %v", safeDbName, err)
		http.Error(w, "Could not create database", http.StatusInternalServerError)
		return
	}

	dbDatabase = safeDbName
	log.Println("Successfully created database:", dbDatabase)

	http.Redirect(w, r, "/home", http.StatusSeeOther)
}
//endregion 

//region use DB handler
func useDb(w http.ResponseWriter, r *http.Request) {
	log.Printf("useDb ishandler called")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	dbDatabase = strings.TrimSpace(r.URL.Query().Get("dbName"))
	if dbDatabase == "" {
		log.Println("Error: missing database name")
		http.Error(w, "Database name is required", http.StatusBadRequest)
		return
	}
	// Close previous connection if it exists
	if db != nil {
		if err := db.Close(); err != nil {
			log.Println("Error closing previous DB connection:", err)
		}
	}
 //build new connection string
	safeDbName := strings.ReplaceAll(dbDatabase, "`", "")
	sqlCon := fmt.Sprintf("%s:%s@tcp(localhost:3306)/%s", dbUser, dbPass, safeDbName)

	newDb, err := sql.Open("mysql", sqlCon)
	if err != nil {
		log.Printf("Error connecting to DB '%s': %v", safeDbName, err)
		http.Error(w, "Failed to switch to database", http.StatusInternalServerError)
		return
	}
	// Test new connection
	if err := newDb.Ping(); err != nil {
		log.Printf("Ping to new DB '%s' failed: %v", safeDbName, err)
		http.Error(w, "Cannot connect to selected database", http.StatusInternalServerError)
		newDb.Close()
		return
	}

	db = newDb
	log.Printf("Switched to database: %s", safeDbName)
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}
//endregion

//region delete DB handler
func deleteDb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dbName := strings.TrimSpace(r.URL.Query().Get("dbName"))
	if dbName == "" {
		http.Error(w, "Database name is required", http.StatusBadRequest)
		log.Println("Error: dbName is empty")
		return
	}
	// Sanitize input by removing backticks to prevent SQL injection
	safeDbName := strings.ReplaceAll(dbName, "`", "")
	// Reset selected DB if it's being deleted
	if dbDatabase == safeDbName {
		dbDatabase = ""
	}

	query := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", safeDbName)
	_, err := db.Exec(query)
	if err != nil {
		log.Printf("Error deleting database '%s': %v", safeDbName, err)
		http.Error(w, "Failed to delete database", http.StatusInternalServerError)
		return
	}

	log.Printf("Database '%s' successfully deleted", safeDbName)
	broadcastQueryToSlaves(query)
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}
//endregion 

//*************************** Table Level **********************************

//region create table handler
func createTable(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	tbName := strings.TrimSpace(r.FormValue("tbName"))
	if tbName == "" {
		http.Error(w, "Missing table name", http.StatusBadRequest)
		return
	}
	log.Println("Received tbName:", tbName) // Debugging

	if db == nil {
		log.Println("DB connection is nil")
		http.Error(w, "No DB connection", http.StatusInternalServerError)
		return
	}
	//
	// Sanitize table name (remove backticks to avoid injection)
	safeTbName := strings.ReplaceAll(tbName, "`", "")
	query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` (id INT AUTO_INCREMENT PRIMARY KEY)", safeTbName)

	_, err := db.Exec(query)
	if err != nil {
		log.Printf("Error creating table %q: %v", safeTbName, err)
		http.Error(w, "Could not create table", http.StatusInternalServerError)
		return
	}
	log.Printf("Created table: %s", safeTbName)
	broadcastQueryToSlaves(query)
	http.Redirect(w, r, "/home", http.StatusSeeOther)

}
//endregion

//region delete table handler 
func deleteTb(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	tableName := strings.TrimSpace(r.URL.Query().Get("tableName"))
	if tableName == "" {
		log.Println("Error: Table name is empty")
		http.Error(w, "Missing table name", http.StatusBadRequest)
		return
	}
	// Sanitize table name to prevent injection
	safeTableName := strings.ReplaceAll(tableName, "`", "")
	query := fmt.Sprintf("DROP TABLE `%s`", safeTableName)

	_, err := db.Exec(query)
	if err != nil {
		log.Printf("Error deleting table %q: %v", safeTableName, err)
		http.Error(w, "Failed to delete table", http.StatusInternalServerError)
		return
	}
	log.Printf("Table deleted: %s", safeTableName)
		broadcastQueryToSlaves(query)
		http.Redirect(w, r, "/home", http.StatusSeeOther)
}
//endregion

//region display table contents handler
func displayTable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	dbTable = strings.TrimSpace(r.URL.Query().Get("tableName"))
	if dbTable == "" {
		log.Println("Error: Table name is empty")
		http.Error(w, "Missing table name", http.StatusBadRequest)
		return
	}

	// Query table contents
	query := fmt.Sprintf("SELECT * FROM `%s`", dbTable)
	rows, err := db.Query(query)
	if err != nil {
		log.Printf("Error retrieving rows from table %s: %v", dbTable, err)
		http.Error(w, "Failed to retrieve table rows", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Println("Error retrieving column names:", err)
		http.Error(w, "Failed to get column names", http.StatusInternalServerError)
		return
	}
// Query column types
	colTypes := []string{}
	typeRows, err := db.Query(fmt.Sprintf("SHOW COLUMNS FROM `%s`", dbTable))
	if err != nil {
		log.Println("Error retrieving column types:", err)
		http.Error(w, "Failed to get column types", http.StatusInternalServerError)
		return
	}
	defer typeRows.Close()

	for typeRows.Next() {
		var field, colType, null, key, def, extra string
		if err := typeRows.Scan(&field, &colType, &null, &key, &def, &extra); err != nil {
			log.Println("Error scanning column metadata:", err)
			continue
		}
		colTypes = append(colTypes, colType)
	}
// Read table rows values 
	var tableRows [][]interface{}
	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			log.Println("Error scanning row:", err)
			continue
		}

		row := make([]interface{}, len(cols))
		for i, val := range values {
			switch v := val.(type) {
			case nil:
				row[i] = "NULL"
			case []byte:
				row[i] = string(v)
			default:
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		tableRows = append(tableRows, row)
	}
	// Check for row iteration error
	if err := rows.Err(); err != nil {
		log.Println("Row iteration error:", err)
		http.Error(w, "Failed during row iteration", http.StatusInternalServerError)
		return
	}
	// Prepare table data
	tbContent := TableContent{
		Columns:  cols,
		ColTypes: colTypes,
		Rows:     tableRows,
		Table:    dbTable,
		ColNum:   len(cols),
		RowNum:   len(tableRows),
	}
	// Render the template with tbContent
	if err := templates.ExecuteTemplate(w, "table.html", tbContent); err != nil {
		log.Println("Template rendering error:", err)
		http.Error(w, "Template rendering failed", http.StatusInternalServerError)
	}
}
//endregion

//*************************** Row Level **********************************

//region add new column handler 
func addCol(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	colName := strings.TrimSpace(r.FormValue("colName"))
	colType := strings.ToLower(strings.TrimSpace(r.FormValue("colType")))
	colNull := r.FormValue("colNull")
	boxValue := strings.TrimSpace(r.FormValue("boxValue"))

	// Validate column name
	if colName == "" {
		http.Error(w, "Invalid column name", http.StatusBadRequest)
		return
	}
	// Handle VARCHAR length
	if colType == "varchar" {
		if boxValue == "" || boxValue == "0" {
			http.Error(w, "VARCHAR must have a valid length", http.StatusBadRequest)
			return
		}
		colType = fmt.Sprintf("VARCHAR(%s)", boxValue)
	}
	// Nullability
	nullability := "NOT NULL"
	if colNull == "true" || colNull == "on" {
		nullability = "NULL"
	}
	// Safely construct the ALTER TABLE query
	query := fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s %s", dbTable, colName, colType, nullability)
	_, err := db.Exec(query)
	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	broadcastQueryToSlaves(query)
	http.Redirect(w, r, "/displayTable?tableName="+dbTable, http.StatusSeeOther)
}

//endregion

//region add new row handler 
func addRow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	tableName := strings.TrimSpace(r.FormValue("tableName"))
	if tableName == "" {
		http.Error(w, "Table name is required", http.StatusBadRequest)
		return
	}
	// Fetch column names
	colsQuery := fmt.Sprintf("SHOW COLUMNS FROM `%s`", tableName)
	rows, err := db.Query(colsQuery)
	if err != nil {
		http.Error(w, "Failed to get table columns: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var field, colType, isNull, key, defaultVal, extra sql.NullString
		if err := rows.Scan(&field, &colType, &isNull, &key, &defaultVal, &extra); err != nil {
			http.Error(w, "Error reading column metadata: "+err.Error(), http.StatusInternalServerError)
			return
		}
		columns = append(columns, field.String)
	}
	// Prepare INSERT components
	var (
		colNames   []string
		placeholders []string
		values     []interface{}
	)

	for _, col := range columns {
		val := r.FormValue(col)
		colNames = append(colNames, fmt.Sprintf("`%s`", col))
		placeholders = append(placeholders, "?")
		if val == "" {
			values = append(values, nil)
		} else {
			values = append(values, val)
		}
	}

	// Parameterized query
	insertQuery := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)",
		tableName,
		strings.Join(colNames, ", "),
		strings.Join(placeholders, ", "),
	)
	if _, err := db.Exec(insertQuery, values...); err != nil {
		http.Error(w, "Insert failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Manual string construction for broadcast
	var valueLiterals []string
	for _, val := range values {
		switch v := val.(type) {
		case nil:
			valueLiterals = append(valueLiterals, "NULL")
		case string:
			escaped := strings.ReplaceAll(v, "'", "''")
			valueLiterals = append(valueLiterals, fmt.Sprintf("'%s'", escaped))
		default:
			valueLiterals = append(valueLiterals, fmt.Sprintf("%v", v))
		}
	}

	broadcastQuery := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)",
		tableName,
		strings.Join(colNames, ", "),
		strings.Join(valueLiterals, ", "),
	)

	broadcastQueryToSlaves(broadcastQuery)
	log.Printf("Added new row to table %s", tableName)
	http.Redirect(w, r, "/displayTable?tableName="+tableName, http.StatusSeeOther)
}
//endregion

//region delete row handler 
func deleteRow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	// Get row ID
	rowID := strings.TrimSpace(r.URL.Query().Get("rowId"))
	if rowID == "" {
		http.Error(w, "Row ID is missing", http.StatusBadRequest)
		return
	}
	// Validate row ID is a number
	if _, err := strconv.Atoi(rowID); err != nil {
		http.Error(w, "Invalid row ID", http.StatusBadRequest)
		return
	}
	// Ensure dbTable is not empty
	if dbTable == "" {
		http.Error(w, "No table selected", http.StatusBadRequest)
		return
	}
	// Safe query execution
	query := fmt.Sprintf("DELETE FROM `%s` WHERE id = ?", dbTable)
	_, err := db.Exec(query, rowID)
	if err != nil {
		log.Printf("Error deleting row: %v", err)
		http.Error(w, "Failed to delete row", http.StatusInternalServerError)
		return
	}
	// Broadcast actual query to slaves (not parameterized)
	broadcastQuery := fmt.Sprintf("DELETE FROM `%s` WHERE id = %s", dbTable, rowID)
	broadcastQueryToSlaves(broadcastQuery)
	log.Printf("Row with id %s deleted from table %s", rowID, dbTable)
	http.Redirect(w, r, "/displayTable?tableName="+dbTable, http.StatusSeeOther)
}
//endregion

//region edit row handler
func editRow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	tableName := strings.TrimSpace(r.FormValue("tableName"))
	if tableName == "" {
		http.Error(w, "Table name is required", http.StatusBadRequest)
		return
	}
	idValueStr := r.FormValue("id")
	if idValueStr == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}
	idValue, err := strconv.Atoi(idValueStr)
	if err != nil {
		http.Error(w, "Invalid ID value", http.StatusBadRequest)
		return
	}
	// get th cols
	colsQuery := fmt.Sprintf("SHOW COLUMNS FROM `%s`", tableName)
	rows, err := db.Query(colsQuery)
	if err != nil {
		http.Error(w, "Failed to get table columns: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var (
		setClauses []string
		values     []interface{}
	)
	for rows.Next() {
		var field, colType, isNull, key, defaultVal, extra sql.NullString
		if err := rows.Scan(&field, &colType, &isNull, &key, &defaultVal, &extra); err != nil {
			http.Error(w, "Error reading column metadata: "+err.Error(), http.StatusInternalServerError)
			return
		}
		fieldName := field.String
		if fieldName == "id" {
			// don't set id
			continue
		}
		val := r.FormValue(fieldName)
		// if the column is empty don't update it
		if val == "" {
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("`%s` = ?", fieldName)) // safer with backticks
		// convert the type based on the type
		if strings.HasPrefix(colType.String, "int") {
			intVal, err := strconv.Atoi(val)
			if err != nil {
				http.Error(w, "Invalid integer value for "+fieldName, http.StatusBadRequest)
				return
			}
			values = append(values, intVal)
		} else if strings.HasPrefix(colType.String, "float") || strings.HasPrefix(colType.String, "double") || strings.HasPrefix(colType.String, "decimal") {
			floatVal, err := strconv.ParseFloat(val, 64)
			if err != nil {
				http.Error(w, "Invalid float value for "+fieldName, http.StatusBadRequest)
				return
			}
			values = append(values, floatVal)
		} else if strings.HasPrefix(colType.String, "bool") || strings.HasPrefix(colType.String, "tinyint(1)") {
			b := 0
			if val == "on" || val == "1" {
				b = 1
			}
			values = append(values, b)
		} else {
			values = append(values, val)
		}
	}
	if len(setClauses) == 0 {
		http.Error(w, "No fields to update", http.StatusBadRequest)
		return
	}
	// add the id value to the where
	values = append(values, idValue)
	// create the query
	query := fmt.Sprintf("UPDATE %s SET %s WHERE id = ?", tableName, strings.Join(setClauses, ", "))

	_, err = db.Exec(query, values...)
	if err != nil {
		http.Error(w, "Update failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// send the query to the slaves
		broadcastQueryToSlaves(query)
	// redirect back to table page
	http.Redirect(w, r, "/displayTable?tableName="+tableName, http.StatusSeeOther)
}
//endregion
//endregion
//
//region startTCPServer funcs
func startTCPServer() {
	listener, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatalf("Failed to start TCP server: %v", err)
		return
	}
	defer listener.Close()
	log.Println("Master TCP server started on port 9000 at", time.Now())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %v", err)
			continue
		}
		registerSlave(conn)
	}
}

func registerSlave(conn net.Conn) {
	clientsMutex.Lock()
	clients[conn] = true
	clientsMutex.Unlock()

	log.Printf("New slave connected: %s", conn.RemoteAddr())

	// Handle slave communication in background
	go syncNewSlave(conn)
	go handleSlave(conn)
}
//endregion

//------------------------------------------------------------------------
func startConsoleAndTCP() {
	defer db.Close()
	startTCPServer()
}

//region syncNewSlave funcs
func syncNewSlave(conn net.Conn) {
	slaveAddr := conn.RemoteAddr().String()
	log.Printf("Starting synchronization for new slave: %s", slaveAddr)

	sendSuccess(conn, "Connected to master successfully. Starting synchronization...")

	if dbDatabase == "" {
		log.Printf("No database selected, skipping sync for slave: %s", slaveAddr)
		sendSuccess(conn, "No database selected. Sync completed.")
		return
	}

	// Step 1: Ensure slave creates and selects the correct database
	sendQuery(conn, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", dbDatabase), "Create database if not exists")
	sendQuery(conn, fmt.Sprintf("USE %s", dbDatabase), "Select database")

	// Step 2: Get tables to sync
	tables, err := fetchTables(dbDatabase)
	if err != nil {
		log.Printf("Error fetching tables for %s: %v", slaveAddr, err)
		sendFailure(conn, "Error getting tables: "+err.Error())
		return
	}
	log.Printf("Found %d tables to sync for slave %s", len(tables), slaveAddr)

	// Step 3: Send schema and data for each table
	for _, table := range tables {
		if err := syncTableSchema(conn, table); err != nil {
			log.Printf("Failed to sync schema for table %s to slave %s: %v", table, slaveAddr, err)
			continue
		}

		syncTableData(conn, table)
	}

	// Step 4: Complete
	sendSuccess(conn, "Database sync completed successfully")
	log.Printf("Completed synchronization for slave: %s", slaveAddr)
}

// Helper to fetch table names
func fetchTables(database string) ([]string, error) {
	rows, err := db.Query("SHOW TABLES FROM " + database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	var tableName string
	for rows.Next() {
		if err := rows.Scan(&tableName); err != nil {
			continue
		}
		tables = append(tables, tableName)
	}
	return tables, nil
}

// Helper to send create table statement
func syncTableSchema(conn net.Conn, table string) error {
	var tableName, tableSchema string
	err := db.QueryRow("SHOW CREATE TABLE " + table).Scan(&tableName, &tableSchema)
	if err != nil {
		return err
	}
	sendQuery(conn, tableSchema, "Create table "+table)
	time.Sleep(100 * time.Millisecond) // Give time to process
	return nil
}

// Convenience wrappers
func sendQuery(conn net.Conn, query, message string) {
	sendMessage(conn, QueryMessage{
		Type:    "query",
		Query:   query,
		Success: true,
		Message: message,
	})
}

func sendSuccess(conn net.Conn, message string) {
	sendMessage(conn, QueryMessage{
		Type:    "response",
		Success: true,
		Message: message,
	})
}

func sendFailure(conn net.Conn, message string) {
	sendMessage(conn, QueryMessage{
		Type:    "response",
		Success: false,
		Message: message,
	})
}
//endregion

//==============
//region  syncTableData funcss
// #region syncTableData
func syncTableData(conn net.Conn, table string) {
	count, err := countTableRows(table)
	if err != nil {
		log.Printf("Error counting rows in %s: %v", table, err)
		return
	}
	if count == 0 {
		log.Printf("Table %s has no data to sync", table)
		return
	}
	log.Printf("Syncing %d rows from table %s", count, table)

	rows, err := db.Query("SELECT * FROM " + table)
	if err != nil {
		log.Printf("Error querying data from %s: %v", table, err)
		return
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		log.Printf("Error retrieving columns for %s: %v", table, err)
		return
	}

	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	rowCounter := 0
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			log.Printf("Error scanning row from %s: %v", table, err)
			continue
		}

		insertQuery := buildInsertQuery(table, columns, values)
		sendMessage(conn, QueryMessage{
			Type:    "query",
			Query:   insertQuery,
			Success: true,
			Message: fmt.Sprintf("Sync data row %d for %s", rowCounter+1, table),
		})

		rowCounter++
		if rowCounter%100 == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	log.Printf("Synced %d rows for table %s", rowCounter, table)
}
// #endregion
// #region countTableRows
func countTableRows(table string) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
	return count, err
}
// #endregion
// #region buildInsertQuery
func buildInsertQuery(table string, columns []string, values []interface{}) string {
	var valueStrings []string
	for _, v := range values {
		switch val := v.(type) {
		case nil:
			valueStrings = append(valueStrings, "NULL")
		case []byte:
			valueStrings = append(valueStrings, fmt.Sprintf("'%s'", strings.ReplaceAll(string(val), "'", "''")))
		case string:
			valueStrings = append(valueStrings, fmt.Sprintf("'%s'", strings.ReplaceAll(val, "'", "''")))
		case time.Time:
			valueStrings = append(valueStrings, fmt.Sprintf("'%s'", val.Format("2006-01-02 15:04:05")))
		default:
			valueStrings = append(valueStrings, fmt.Sprintf("%v", val))
		}
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(columns, ", "), strings.Join(valueStrings, ", "))
}
// #endregion

//endregion 

// #region sendMessage
func sendMessage(conn net.Conn, msg QueryMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message for %s: %v", conn.RemoteAddr(), err)
		return
	}

	// Append newline to serve as message delimiter
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		log.Printf("Failed to send message to %s: %v", conn.RemoteAddr(), err)
	}
}
// #endregion

//-------------------------------- Main func --------------------------------------------------
//region Main Func
func main() {
	// Set up logging
	logFile, err := os.OpenFile("master.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	// Public routes
	http.HandleFunc("/", renderLoginPage)
	http.HandleFunc("/welcome", serveWelcomePage)

	// Admin routes
	http.HandleFunc("/login", login)
	http.HandleFunc("/home", home)

	// Database management
	http.HandleFunc("/createDb", createDb)
	http.HandleFunc("/useDb", useDb)
	http.HandleFunc("/deleteDb", deleteDb)

	// Table management
	http.HandleFunc("/createTable", createTable)
	http.HandleFunc("/deleteTb", deleteTb)
	http.HandleFunc("/displayTable", displayTable)
	http.HandleFunc("/addCol", addCol)
	http.HandleFunc("/addRow", addRow)
	http.HandleFunc("/deleteRow", deleteRow)
	http.HandleFunc("/editRow", editRow)

	// Static file handler (CSS, JS, etc.)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Start the server
	fmt.Println("Server started on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}

// Helper route handler
func renderLoginPage(w http.ResponseWriter, r *http.Request) {
	if err := templates.ExecuteTemplate(w, "login.html", nil); err != nil {
		http.Error(w, "Error rendering login page", http.StatusInternalServerError)
		log.Println("Template execution error:", err)
	}
}

func serveWelcomePage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/welcome.html")
}
//endregion

// displayTableMenu prompts the user to enter a table name.
// Returns false if the user enters "-1" (exit condition), otherwise true.
func displayTableMenu() bool {
	fmt.Println("===================================")
	fmt.Println("Enter the Table Name or -1 If You Don't Want to Create More:")
	fmt.Println("===================================")

	input, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Error reading input:", err)
		return false
	}

	dbTable = strings.TrimSpace(input)
	return dbTable != "-1"
}

//============
// displayColumnsMenu prompts the user to enter a column definition.
// Returns false if the user enters "-1", indicating they don't want to add more columns.
func displayColumnsMenu() bool {
	fmt.Println("===================================")
	fmt.Println("Enter the Column Definition (name type attributes), or -1 to stop:")
	fmt.Println("===================================")

	input, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Error reading input:", err)
		return false
	}

	dbColumn = strings.TrimSpace(input)
	return dbColumn != "-1"
}
//===============

// #region handleSlave

func handleSlave(conn net.Conn) {
	defer func() {
		conn.Close()
		clientsMutex.Lock()
		delete(clients, conn)
		clientsMutex.Unlock()
		log.Printf("Slave disconnected: %s\n", conn.RemoteAddr())
		fmt.Println("Slave disconnected:", conn.RemoteAddr())
	}()

	sqlCon := fmt.Sprintf("%s:%s@tcp(localhost:3306)/%s", dbUser, dbPass, dbDatabase)
	db, err := sql.Open("mysql", sqlCon)
	if err != nil {
		log.Println("Database connection error for slave handler:", err)
		return
	}
	defer db.Close()

	sendMessage(conn, QueryMessage{
		Type:    "response",
		Success: true,
		Message: "Connected to master successfully",
	})

	reader := bufio.NewReader(conn)
	for {
		message, err := reader.ReadBytes('\n')
		if err != nil {
			log.Printf("Connection closed or read error from %s: %v\n", conn.RemoteAddr(), err)
			return
		}

		var queryMsg QueryMessage
		if err := json.Unmarshal(message, &queryMsg); err != nil {
			log.Println("Error parsing message:", err)
			sendErrorResponse(conn, "Invalid message format")
			continue
		}

		query := strings.TrimSpace(queryMsg.Query)
		if query == "" {
			sendErrorResponse(conn, "Empty query")
			continue
		}

		log.Printf("Received query from %s: %s\n", conn.RemoteAddr(), query)

		lowerQuery := strings.ToLower(query)
		if strings.HasPrefix(lowerQuery, "create") || strings.HasPrefix(lowerQuery, "drop") {
			sendErrorResponse(conn, "Error: Slave is not allowed to perform CREATE or DROP operations")
			log.Printf("Rejected restricted query from slave: %s\n", query)
			continue
		}

		if strings.HasPrefix(lowerQuery, "select") {
			handleSelectQuery(conn, db, query)
		} else {
			handleExecQuery(conn, db, query)
		}
	}
}

// #endregion

// #region handleSelectQuery commented 

// func handleSelectQuery(conn net.Conn, db *sql.DB, query string) {
// 	rows, err := db.Query(query)
// 	if err != nil {
// 		log.Println("Query error:", err)
// 		sendErrorResponse(conn, "Error: "+err.Error())
// 		return
// 	}
// 	defer rows.Close()

// 	cols, err := rows.Columns()
// 	if err != nil {
// 		log.Println("Error fetching columns:", err)
// 		sendErrorResponse(conn, "Error fetching columns: "+err.Error())
// 		return
// 	}

// 	values := make([]sql.RawBytes, len(cols))
// 	scanArgs := make([]interface{}, len(values))
// 	for i := range values {
// 		scanArgs[i] = &values[i]
// 	}

// 	var builder strings.Builder
// 	// Write column headers
// 	builder.WriteString(strings.Join(cols, "\t") + "\n")

// 	rowCount := 0
// 	for rows.Next() {
// 		if err := rows.Scan(scanArgs...); err != nil {
// 			log.Println("Row scan error:", err)
// 			continue
// 		}
// 		rowCount++

// 		for i, val := range values {
// 			if i > 0 {
// 				builder.WriteString("\t")
// 			}
// 			if val == nil {
// 				builder.WriteString("NULL")
// 			} else {
// 				builder.WriteString(string(val))
// 			}
// 		}
// 		builder.WriteString("\n")
// 	}

// 	builder.WriteString(fmt.Sprintf("\n%d row(s) returned", rowCount))

// 	sendMessage(conn, QueryMessage{
// 		Type:    "response",
// 		Success: true,
// 		Query:   builder.String(),
// 		Message: "Query executed successfully",
// 	})
// 	log.Printf("Sent SELECT response to %s\n", conn.RemoteAddr())
// }

// #endregion

// #region handleExecQuery

// Helper function for non-SELECT queries (INSERT, UPDATE, DELETE, etc.)
func handleExecQuery(conn net.Conn, db *sql.DB, query string) {
	result, err := db.Exec(query)
	if err != nil {
		log.Println("Execution error:", err)
		sendErrorResponse(conn, "Error: "+err.Error())
		return
	}

	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()

	// Send success response to the requesting slave
	sendMessage(conn, QueryMessage{
		Type:    "response",
		Success: true,
		Query:   fmt.Sprintf("Rows affected: %d", rowsAffected),
		Message: "Query executed successfully",
	})

	log.Printf("Executed successfully for %s. Rows: %d, LastID: %d",
		conn.RemoteAddr(), rowsAffected, lastInsertID)

	// Broadcast the query to other slaves (except the originator)
	broadcastQueryToOtherSlaves(query, conn)
}

// #endregion

// #region sendErrorResponse

// Helper function to send error responses to the slave connection
func sendErrorResponse(conn net.Conn, message string) {
	response := QueryMessage{
		Type:    "response",
		Success: false,
		Message: message,
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		log.Println("Error marshalling error response:", err)
		return
	}
	responseJSON = append(responseJSON, '\n')
	_, err = conn.Write(responseJSON)
	if err != nil {
		log.Println("Error sending error response:", err)
	}
}

// #endregion

// #region broadcastQueryToSlaves

// Broadcast a query to all connected slave clients
func broadcastQueryToSlaves(query string) {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	if len(clients) == 0 {
		log.Println("No slaves connected to broadcast query")
		return
	}

	log.Printf("Broadcasting query to %d slaves: %s", len(clients), query)

	message := QueryMessage{
		Type:  "query",
		Query: query,
	}
	messageJSON, err := json.Marshal(message)
	if err != nil {
		log.Println("Error creating JSON message:", err)
		return
	}
	messageJSON = append(messageJSON, '\n')

	for conn := range clients {
		_, err := conn.Write(messageJSON)
		if err != nil {
			log.Printf("Error sending query to slave %s: %v", conn.RemoteAddr(), err)
			// Do not remove client here to avoid concurrent map writes; cleanup handled elsewhere
		} else {
			log.Printf("Sent query to slave: %s", conn.RemoteAddr())
		}
	}
}

// #endregion

// #region broadcastQueryToOtherSlaves

// Broadcasts a query to all connected slaves except the origin connection
func broadcastQueryToOtherSlaves(query string, originConn net.Conn) {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	count := len(clients) - 1 // Exclude the origin connection
	if count <= 0 {
		log.Println("No other slaves connected to broadcast query")
		return
	}

	log.Printf("Broadcasting query to %d other slaves: %s", count, query)

	message := QueryMessage{
		Type:  "query",
		Query: query,
	}
	messageJSON, err := json.Marshal(message)
	if err != nil {
		log.Println("Error creating JSON message:", err)
		return
	}
	messageJSON = append(messageJSON, '\n')

	for conn := range clients {
		if conn != originConn { // Skip origin connection
			_, err := conn.Write(messageJSON)
			if err != nil {
				log.Printf("Error sending query to slave %s: %v", conn.RemoteAddr(), err)
			} else {
				log.Printf("Sent query to slave: %s", conn.RemoteAddr())
			}
		}
	}
}

// #endregion

// #region broadcastQuery

// Broadcasts a query with a synchronization prefix to all connected slaves except the sender
func broadcastQuery(query string, sender net.Conn) {
	syncQuery := "SYNC_QUERY;" + query + ";\n"

	for client := range clients {
		if client != sender { // Skip the sender
			_, err := client.Write([]byte(syncQuery))
			if err != nil {
				log.Println("Error broadcasting to slave:", err)
				// Avoid removing client here to prevent concurrent map access
			}
		}
	}
	log.Println("Query broadcasted to all slaves")
}

// #endregion

// #region handleSelectQuery

func handleSelectQuery(conn net.Conn, db *sql.DB, query string) {
    rows, err := db.Query(query)
    if err != nil {
        log.Println("Query error:", err)
        sendErrorResponse(conn, "Error: "+err.Error())
        return
    }
    defer rows.Close()

    cols, err := rows.Columns()
    if err != nil {
        log.Println("Error fetching columns:", err)
        sendErrorResponse(conn, "Error fetching columns: "+err.Error())
        return
    }

    values := make([]sql.RawBytes, len(cols))
    scanArgs := make([]interface{}, len(values))
    for i := range values {
        scanArgs[i] = &values[i]
    }

    var builder strings.Builder
    builder.WriteString(strings.Join(cols, "\t") + "\n")

    rowCount := 0
    for rows.Next() {
        if err := rows.Scan(scanArgs...); err != nil {
            log.Println("Row scan error:", err)
            continue
        }
        rowCount++
        for i, val := range values {
            if i > 0 {
                builder.WriteString("\t")
            }
            if val == nil {
                builder.WriteString("NULL")
            } else {
                builder.WriteString(string(val))
            }
        }
        builder.WriteString("\n")
    }

    builder.WriteString(fmt.Sprintf("\n%d row(s) returned", rowCount))

    sendMessage(conn, QueryMessage{
        Type:    "response",
        Success: true,
        Query:   builder.String(),
        Message: "Query executed successfully",
    })
    log.Printf("Sent SELECT response to %s\n", conn.RemoteAddr())
}

// #endregion

