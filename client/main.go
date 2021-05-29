package main

import (
	"flag"
	"log"
	"time"

	swarm "github.com/Kh4n/winter-is-coming-submission"
)

func main() {
	msg := flag.String("msg", "", "the message to send the other peer")
	flag.Parse()
	if *msg == "" {
		log.Fatal("You must provide a message for the other peer")
	}
	log.Fatal(swarm.SimpleHolepunch(*msg, time.Second*20))
}
