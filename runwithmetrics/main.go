package main

import (
	"fmt"
	_ "github.com/heroku/x/hmetrics/onload"
	"github.com/webhookdb/icalproxy/cmd"
)

func main() {
	fmt.Println("starting_with_metrics")
	cmd.Execute()
}
