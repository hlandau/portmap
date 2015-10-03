NAT Port Mapping Library for Go
===============================

[![GoDoc](https://godoc.org/github.com/hlandau/portmap?status.svg)](https://godoc.org/github.com/hlandau/portmap) [![Build Status](https://travis-ci.org/hlandau/portmap.svg?branch=master)](https://travis-ci.org/hlandau/portmap)

Example:

```go
import "github.com/hlandau/portmap"
import "fmt"

func Example() {
  m, err := portmap.New(portmap.Config{
    Protocol:     portmap.TCP,
    Name:         "http",
    InternalPort:  80,
    ExternalPort:  80,
  })
  if err != nil {
    // ...
  }

  for {
    // mapping may change over time
    <-m.NotifyChan()
    fmt.Printf("Current mapped address is: %s\n", m.ExternalAddr())
  }

  // mapping will be renewed automatically
  // call m.Delete() when mapping should be torn down
}
```
