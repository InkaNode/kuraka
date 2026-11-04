package migrate

import (
	"strings"

	"github.com/emersion/go-imap/v2"
)

var specialUses = []imap.MailboxAttr{
	imap.MailboxAttrSent, imap.MailboxAttrDrafts, imap.MailboxAttrTrash,
	imap.MailboxAttrJunk, imap.MailboxAttrArchive,
}

func hasAttr(folder *imap.ListData, attr imap.MailboxAttr) bool {
	for _, candidate := range folder.Attrs {
		if strings.EqualFold(string(candidate), string(attr)) {
			return true
		}
	}
	return false
}

func explicitSpecialUse(folder *imap.ListData) imap.MailboxAttr {
	for _, attr := range specialUses {
		if hasAttr(folder, attr) {
			return attr
		}
	}
	return ""
}

func specialUse(folder *imap.ListData) imap.MailboxAttr {
	if attr := explicitSpecialUse(folder); attr != "" {
		return attr
	}
	name := folder.Mailbox
	if folder.Delim != 0 {
		parts := strings.Split(name, string(folder.Delim))
		name = parts[len(parts)-1]
	}
	switch strings.ToLower(name) {
	case "sent", "sent items", "enviados":
		return imap.MailboxAttrSent
	case "drafts", "borradores":
		return imap.MailboxAttrDrafts
	case "trash", "deleted items", "papelera":
		return imap.MailboxAttrTrash
	case "junk", "spam":
		return imap.MailboxAttrJunk
	case "archive":
		return imap.MailboxAttrArchive
	}
	return ""
}

func convertDelimiter(name string, src, dst rune) string {
	if src == 0 || dst == 0 || src == dst {
		return name
	}
	return strings.ReplaceAll(name, string(src), string(dst))
}

func mapFolder(src *imap.ListData, targets []*imap.ListData, delim rune) string {
	if strings.EqualFold(src.Mailbox, "INBOX") {
		return "INBOX"
	}
	if attr := specialUse(src); attr != "" {
		// Server attributes take precedence over guesses from localized names.
		for _, infer := range []bool{false, true} {
			for _, target := range targets {
				if hasAttr(target, imap.MailboxAttrNoSelect) {
					continue
				}
				if (!infer && hasAttr(target, attr)) || (infer && specialUse(target) == attr) {
					return target.Mailbox
				}
			}
		}
	}
	return convertDelimiter(src.Mailbox, src.Delim, delim)
}

// folderPaths returns parents before their children. A NIL delimiter denotes a
// flat namespace, where delimiter-like characters are ordinary name characters.
func folderPaths(name string, delim rune) []string {
	paths := []string{}
	if delim != 0 {
		for i, r := range name {
			if r == delim && i > 0 {
				paths = append(paths, name[:i])
			}
		}
	}
	return append(paths, name)
}

func targetDelimiter(folders []*imap.ListData) rune {
	for _, folder := range folders {
		if strings.EqualFold(folder.Mailbox, "INBOX") {
			return folder.Delim
		}
	}
	if len(folders) > 0 {
		return folders[0].Delim
	}
	return 0
}
