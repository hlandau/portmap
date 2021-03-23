NAT Port Mapping Library for Go
===============================

[![godocs.io](https://godocs.io/github.com/hlandau/portmap?status.svg)](https://godocs.io/github.com/hlandau/portmap) [![Build status](https://github.com/hlandau/portmap/actions/workflows/go.yml/badge.svg)](#) [![No modules](https://www.devever.net/~hl/f/no-modules2.svg) 100% modules-free.](https://www.devever.net/~hl/gomod)

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
