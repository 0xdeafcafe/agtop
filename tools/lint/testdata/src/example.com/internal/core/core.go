package core

import "example.com/internal/adapters/fake" // want "the core imports no adapter"

var _ = fake.Name
