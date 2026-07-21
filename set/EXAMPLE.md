# `set` example

```go
import "github.com/pliu/go-utils/set"

seen := set.NewSet[string]()
seen.Add("alpha")
seen.Add("beta")

present := seen.Contains("alpha") // true
items := seen.Items()             // iteration order is unspecified
seen.Remove("beta")
```

Duplicate additions do not change the set's length. `Equals` compares two
sets without depending on their iteration order.
