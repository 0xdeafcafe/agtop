package host

// restoreQueue runs before the first publish overwrites the previous host's
// snapshot. UI reloads already retain their live host; this also handles an
// explicit host restart or a crash. Never carry work into a different branch.
func (s *server) restoreQueue(old Info) {
	if old.ID != s.cfg.ID || old.Kind != s.cfg.Kind || old.SessionID != s.cfg.SessionID || s.cfg.Fork {
		return
	}
	s.info.Queue = old.Queue
	s.info.QueueImages = old.QueueImages
	s.info.QueueExchanges = old.QueueExchanges
	s.info.QueueHeld = old.QueueHeld
	s.info.QueueSeparate = old.QueueSeparate
	if len(old.Queue) == 0 {
		return
	}
	s.info.Limit = old.Limit
	// The initial prompt has already been submitted if later messages queued.
	// It must not be replayed ahead of that recovered work.
	s.cfg.Prompt, s.cfg.Images, s.cfg.PromptExchange = "", nil, nil
	if s.cfg.SessionID != "" {
		s.cfg.Resume, s.began = true, true
	}
	s.saveConfig()
}
