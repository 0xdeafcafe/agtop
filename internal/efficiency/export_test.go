package efficiency

// For the tests outside the package, which read Claude Code's files
// through its adapter.

var (
	SearchPath     = &searchPath
	CacheReadPrice = cacheReadPrice
)

func (f *File) Clone() *File    { return f.clone() }
func (s *Store) Retire(f *File) { s.retire(f) }

// StoreOf is a store holding files, and nothing retired.
func StoreOf(files map[string]*File) *Store {
	return &Store{files: files, retired: map[string]*Retired{}}
}
