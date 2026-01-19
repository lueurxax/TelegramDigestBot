package main

import (
	"fmt"
	"net/http"
)

func main() {
	fmt.Println(http.CanonicalMIMEHeaderKey("WWW-Authenticate"))
	fmt.Println(http.CanonicalMIMEHeaderKey("Www-Authenticate"))
}
