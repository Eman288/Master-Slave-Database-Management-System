[img](demo.png)
# Master-Slave Database Management System

A full-stack simulation of a **Master-Slave Database Architecture**, implemented in **Go (Golang)** with a web frontend using **Go HTML templates** and **MySQL** as the backend database. This project demonstrates how distributed systems manage read/write operations across a master and one or more slave databases.

## 📺 Live Demo

👉 **[Watch the demo](https://eman288.github.io/Master-Slave-Database-Management-System/)**  

---

## 📖 Table of Contents

- [About the Project](#about-the-project)
- [Features](#features)
- [Technologies Used](#technologies-used)
- [Getting Started](#getting-started)
- [Usage](#usage)
- [Project Structure](#project-structure)
- [Contributing](#contributing)
- [License](#license)
- [Contact](#contact)

---

## About the Project

This project simulates how a **Master-Slave database architecture** works in distributed systems:

- **Master** is responsible for creating and dropping databases and tables, and can do the CRUD operations on the tables.
- **Slaves** are only responsible for doing the CRUD operations, each has a copy of the master current database and they all work on it at the same time while handling them, so no race condition can happen.
- After any query, both the slave and master will hear the change that happened in either  of them.

The goal is to demonstrate how replication improves performance and reliability, and to serve as an educational tool for students and developers learning about high-availability database systems.

---

## Features

✅ Master-Driven Schema Management: The Master node can create/drop databases and tables.

✅ Distributed CRUD Operations: Both Master and Slaves can perform CRUD operations concurrently on synchronized copies of the database.

✅ Synchronized State Awareness: Any change (insert, update, delete) on one node (Master or Slave) is immediately visible to all others.

✅ No Race Conditions: All operations are safely handled in a way that avoids race conditions between nodes.

✅ Real-Time Replication: Data is replicated instantly between Master and Slaves after any query execution.

✅ Fault Tolerance Simulation: The architecture models how replication can increase availability and reduce single points of failure.

✅ Web-Based Interface: Interact with the system via a clean and simple web UI built using Go’s HTML templates.

✅ Lightweight and Educational: Simple structure suitable for learning and experimenting with distributed database concepts.

---

## Technologies Used

- **Backend**: Go (Golang)
  - `net/http` — web server
  - `html/template` — frontend rendering
  - `database/sql` — database access
  - [`go-sql-driver/mysql`](https://github.com/go-sql-driver/mysql) — MySQL driver
- **Frontend**: HTML templates rendered by Go
- **Database**: MySQL (1 Master + 1 or more Slaves)
- **Deployment**: GitHub Pages (for demo video or UI showcase)

---

## Getting Started

### Prerequisites

Make sure you have the following installed:

- [Go](https://golang.org/dl/) (v1.18+ recommended)
- [MySQL Server](https://dev.mysql.com/downloads/mysql/)
- Git

## 🚀 Installation

1. **Clone the repository:**

```bash
git clone https://github.com/Eman288/Master-Slave-Database-Management-System.git
cd Master-Slave-Database-Management-System
```

2. **Set up MySQL Servers:**

* You will need **two MySQL servers** running locally — one for the **Master**, one for the **Slave**.
* Ensure both are properly configured and accessible.

3. **Run the Master Server:**

```bash
go run master.go
```

* A GUI will appear.
* Enter your **MySQL username and password** through the GUI login form.
* Once authenticated, the home page will display all available databases.
* You can now:

  * Create a new database
  * Drop existing databases
  * Select a database and manage its tables (CRUD)

4. **Run the Slave Server (Console-Based):**

```bash
go run slave.go
```

* You will be prompted in the **console** to enter:

  * MySQL username
  * MySQL password
  * The **name of the database** to connect to (must match a database on the Master side)
* If the database or tables do not exist locally, they will be automatically created based on the Master.

5. Once both Master and Slave are running and connected to the same logical database:

   * The system is fully synchronized.
   * All future queries will be executed on **both** Master and Slave servers in real time.

---

## ⚙️ Usage

* Use the **Master GUI** to manage databases and tables:

  * Create or drop databases
  * Create tables
  * Insert, update, or delete data
    
* The **Slave**, via the console, will:

  * Automatically replicate the chosen database if it doesn’t exist
  * Receive and execute all queries issued from the Master side
* Any change made on either side (Master or Slave) is **immediately reflected** on the other.
* This simulates a live, bidirectional sync between Master and Slave databases.
* All logs and synchronization details are shown in the terminal (console).

## Project Structure

```
Master-Slave-Database-Management-System/
├── templates/
│   ├── welcome.html      # Main form UI
│   └── table.html        # display table content
│   └── login.html        # Login page for the master
│   └── home.html        # Display databases and table in the database currently selected
├── static/               # Optional CSS
│   └── login.css
│   └── table.css
│   └── home.css
│   └── welcome.css
├── master.go                # Go server for the master
├── slave.go                # Go server for the slave (run on a different pc)
├── go.mod / go.sum        # Dependencies
├── demo.mp4               # (Optional) Video demo
└── README.md              # This file
```

---

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.

---

## Contact

**Eman**
GitHub: [@Eman288](https://github.com/Eman288)
**Eman**
GitHub: [@Eman288](https://github.com/Eman288)
**Eman**
GitHub: [@Eman288](https://github.com/Eman288)
**Eman**
GitHub: [@Eman288](https://github.com/Eman288)
**Eman**
GitHub: [@Eman288](https://github.com/Eman288)
**Eman**
GitHub: [@Eman288](https://github.com/Eman288)

Feel free to reach out for feedback, questions, or collaboration ideas!

