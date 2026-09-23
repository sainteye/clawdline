// Command deploy-limit prints the registered Linux release health deadline.
// The shell deploy reads this rather than carrying a second spelling of the
// bound outside the capacity register.
package main

import (
	"fmt"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func main() {
	fmt.Println(capacity.Default(capacity.DeployHealthLimit))
}
