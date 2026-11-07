package main

import "flag"

func main() {
	flag.String("c", "/etc/ewdbd.conf", "config path")
	flag.Parse()
}
