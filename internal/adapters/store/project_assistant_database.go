package store

import "database/sql"

// ProjectAssistantDatabase lends the existing writer to the internal feature
// host. The caller must not close it or create another feature Store for it.
func (s *Store) ProjectAssistantDatabase() *sql.DB { return s.db }

// ProjectAssistantReader lends the existing query-only pool. Its lifetime and
// bounds belong to Store; feature readers must never close or reconfigure it.
func (s *Store) ProjectAssistantReader() *sql.DB { return s.rd }
