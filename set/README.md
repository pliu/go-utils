# `set`

Generic set for comparable Go values.

`Set` ignores duplicate additions and also provides `Len` and `Equals`.

## Example

```go
import "github.com/pliu/go-utils/set"

seen := set.NewSet[string]()
seen.Add("alpha")
seen.Add("beta")

present := seen.Contains("alpha") // true
items := seen.Items()             // iteration order is unspecified
seen.Remove("beta")
```

`Equals` compares two sets without depending on their iteration order.
