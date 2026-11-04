package migrate

import "time"

// Progress is a cumulative snapshot of a migration. Folder names refer to the
// source; MessagesTotal includes duplicates in all selectable source folders.
type Progress struct {
	FoldersTotal    int
	FoldersDone     int
	MessagesTotal   int
	MessagesCopied  int
	MessagesSkipped int
	Folder          string
}

type progressReporter struct {
	report func(Progress)
	now    func() time.Time
	last   time.Time
}

func (r *progressReporter) emit(p Progress, folderEnd bool) {
	if r.report == nil {
		return
	}
	now := r.now()
	if folderEnd || r.last.IsZero() || now.Sub(r.last) >= 250*time.Millisecond {
		r.report(p)
		r.last = now
	}
}
