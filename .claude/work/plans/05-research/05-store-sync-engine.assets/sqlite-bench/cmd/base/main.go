package main

import (
	"database/sql"
	"fmt"
)

func main() { fmt.Println(sql.Drivers()) }
