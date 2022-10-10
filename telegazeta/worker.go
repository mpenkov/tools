package telegazeta

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

//
// Dump messages for collecting test data
//
func dump(message *tg.Message, path string) error {
	if path == "" {
		return nil
	}
	var buf bin.Buffer
	err := message.Encode(&buf)
	if err != nil {
		return err
	}

	filepath := fmt.Sprintf("%s/%d.bin", path, message.ID)
	fout, err := os.OpenFile(filepath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer fout.Close()
	fout.Write(buf.Buf)

	return nil
}

type Worker struct {
	Context         context.Context
	Log             *zap.Logger
	Client          *tg.Client
	TmpPath         string
	DumpPath        string
	DurationSeconds int64

	channelCache    map[int64]Channel
}

func (w Worker) getChannelInfo(input tg.InputChannelClass) (Channel, error) {
	var result *tg.MessagesChatFull
	var err error

	result, err = w.Client.ChannelsGetFullChannel(w.Context, input)
	if err != nil {
		return Channel{}, fmt.Errorf("ChannelsGetFullChannel failed: %w", err)
	}

	switch thing := result.Chats[0].(type) {
	case *tg.Chat:
		return Channel{Title: thing.Title, Domain: ""}, nil
	case *tg.Channel:
		return Channel{Title: thing.Title, Domain: thing.Username}, nil
	}

	return Channel{}, fmt.Errorf("not implemented yet")
}

func (w Worker) downloadThumbnail(location tg.InputFileLocationClass) (string, error) {
	var id int64
	switch location := location.(type) {
	case *tg.InputPhotoFileLocation:
		id = location.ID
	case *tg.InputDocumentFileLocation:
		id = location.ID
	default:
		return "", fmt.Errorf("unable to determine object ID from %s", location.String())
	}

	path := fmt.Sprintf("/%s/%d.jpeg", w.TmpPath, id)
	_, err := os.Stat(path)
	if err == nil {
		//
		// File exists, we don't need to download
		//
		return path, nil
	} else {
		w.Log.Info(fmt.Sprintf("downloading thumbnail for id %d", id))
		dloader := downloader.NewDownloader()
		builder := dloader.Download(w.Client, location)
		_, err := builder.ToPath(w.Context, path)
		if err == nil {
			return path, nil
		} else {
			return "", err
		}
	}
}

func (w Worker) predownloadDocumentThumbnail(doc *tg.Document) (Media, error) {
	var media Media
	if strings.HasPrefix(doc.MimeType, "video/") {
		media.IsVideo = true
		for _, attr := range doc.Attributes {
			switch a := attr.(type) {
			case *tg.DocumentAttributeVideo:
				minutes := a.Duration / 60
				seconds := a.Duration % 60
				media.Duration = fmt.Sprintf("%02d:%02d", minutes, seconds)
			}
		}
	}

	thumbnailSize, err := bestThumbnailSize(doc.Thumbs)
	w.Log.Info(fmt.Sprintf("doc.ID: %d thumbnailSize: %s", doc.ID, thumbnailSize.String()))
	if err == nil {
		media.ThumbnailWidth = thumbnailSize.W
		media.ThumbnailHeight = thumbnailSize.H
		media.PendingDownload = &tg.InputDocumentFileLocation{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
			ThumbSize:     thumbnailSize.Type,
		}
	}

	return media, err
}

func (w Worker) predownloadPhotoThumbnail(photo *tg.Photo) (Media, error) {
	var media Media
	thumbnailSize, err := bestThumbnailSize(photo.Sizes)
	if err == nil {
		media.PendingDownload = &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     thumbnailSize.Type,
		}
		media.ThumbnailWidth = thumbnailSize.W
		media.ThumbnailHeight = thumbnailSize.H
	}
	return media, err
}

func (w Worker) processMessage(m tg.Message) (Item, error) {
	var webpage *tg.WebPage
	var media Media
	var haveMedia bool = false

	if m.Media != nil {
		w.Log.Debug("attachment: " + m.Media.TypeName())

		switch messageMedia := m.Media.(type) {
		case *tg.MessageMediaPhoto:
			switch photo := messageMedia.Photo.(type) {
			case *tg.Photo:
				var err error
				media, err = w.predownloadPhotoThumbnail(photo)
				if err != nil {
					w.Log.Error(fmt.Sprintf("unable to downloadDocumentThumbnail: %s", err))
				} else {
					haveMedia = true
				}
			}
		case *tg.MessageMediaWebPage:
			switch wp := messageMedia.Webpage.(type) {
			case *tg.WebPage:
				//
				// Quoting another telegram channel?
				// Two places to look here: .Document and .Photo.
				// Either one will do.
				//
				webpage = wp
				switch doc := wp.Document.(type) {
				case *tg.Document:
					var err error
					media, err = w.predownloadDocumentThumbnail(doc)
					if err != nil {
						w.Log.Error(fmt.Sprintf("unable to downloadDocumentThumbnail: %s", err))
					} else {
						haveMedia = true
					}
				}

				if !haveMedia {
					switch photo := wp.Photo.(type) {
					case *tg.Photo:
						var err error
						media, err = w.predownloadPhotoThumbnail(photo)
						if err != nil {
							w.Log.Error(fmt.Sprintf("unable to downloadDocumentThumbnail: %s", err))
						} else {
							haveMedia = true
						}
					}
				}
			}
		case *tg.MessageMediaDocument:
			switch doc := messageMedia.Document.(type) {
			case *tg.Document:
				var err error
				media, err = w.predownloadDocumentThumbnail(doc)
				if err != nil {
					w.Log.Error(fmt.Sprintf("unable to downloadDocumentThumbnail: %s", err))
				} else {
					haveMedia = true
				}
			}
		}
	}

	item := newItem(&m)
	if webpage != nil {
		item.Webpage = webpage
		item.HasWebpage = true
	}
	if haveMedia {
		item.Media = append(item.Media, media)
	}

	return item, nil
}

func (w Worker) paginateMessages(ip tg.InputPeerClass) ([]tg.Message, error) {
	//
	// Page through the message history until we reach messages that are too old.
	// Telegram typically serves 20 messages per request.
	//
	thresholdDate := time.Now().Unix() - w.DurationSeconds
	offset := 0
	messages := []tg.Message{}
	for {
		var history tg.MessagesMessagesClass
		var err error
		getHistoryRequest := tg.MessagesGetHistoryRequest{Peer: ip, AddOffset: offset}
		history, err = w.Client.MessagesGetHistory(w.Context, &getHistoryRequest)
		if err != nil {
			return messages, err
		}

		response := w.decodeMessages(history)

		stop := false
		for _, m := range response {
			if int64(m.Date) < thresholdDate {
				stop = true
				break
			} else {
				messages = append(messages, m)
			}
		}

		if stop {
			break
		} else {
			offset += len(response)
		}
	}
	return messages, nil
}

func (w Worker) decodeMessages(mmc tg.MessagesMessagesClass) (messages []tg.Message) {
	var innerMessages []tg.MessageClass

	switch inner := mmc.(type) {
	case *tg.MessagesChannelMessages:
		innerMessages = inner.Messages
	case *tg.MessagesMessagesSlice:
		innerMessages = inner.Messages
	}

	for _, m := range innerMessages {
		w.Log.Debug(m.String())
		switch message := m.(type) {
		case *tg.Message:
			messages = append(messages, *message)
			dump(message, w.DumpPath)
		}
	}
	return messages
}

func (w Worker) processPeer(ip tg.InputPeerClass) ([]Item, error) {
	var items []Item

	var err error

	inputPeerChannel, ok := ip.(*tg.InputPeerChannel)
	if !ok {
		return []Item{}, fmt.Errorf("unable to processPeer: %s", ip)
	}
	inputChannel := tg.InputChannel{
		AccessHash: inputPeerChannel.AccessHash,
		ChannelID:  inputPeerChannel.ChannelID,
	}
	channel, err := w.getChannelInfo(&inputChannel)
	if err != nil {
		return []Item{}, fmt.Errorf("unable to getChannelInfo: %w", err)
	}

	w.channelCache[inputChannel.ChannelID] = channel

	messages, err := w.paginateMessages(ip)
	if err != nil {
		return []Item{}, fmt.Errorf("unable to paginateMessages: %w", err)
	}

	for _, m := range messages {
		item, _ := w.processMessage(m)
		item.Channel = channel

		if fwdFrom, ok := m.GetFwdFrom(); ok {
			var chid int64

			switch fromPeer := fwdFrom.FromID.(type) {
			case *tg.PeerChannel:
				chid = fromPeer.ChannelID
			}

			if channelInfo, ok := w.channelCache[chid]; ok {
				item.FwdFrom = channelInfo
				item.Forwarded = true
			} else {
				inputChannel := tg.InputChannelFromMessage{
					ChannelID: chid,
					MsgID:     m.ID,
					Peer:      ip,
				}

				fwdFrom, err := w.getChannelInfo(&inputChannel)
				if err == nil {
					item.FwdFrom = fwdFrom
					item.Forwarded = true
					w.channelCache[chid] = fwdFrom
				}
			}
		}
		for i := range item.Media {
			item.Media[i].URL = tgUrl(item)
		}

		items = append(items, item)

		w.Log.Info(
			fmt.Sprintf(
				"handled MessageID %d (%s) from Domain %s (%d char)",
				item.MessageID,
				item.Date,
				item.Channel.Domain,
				len(item.Text),
			),
		)
	}

	return items, nil
}

func (w Worker) Collect(channels []string) ItemList {
	w.channelCache = make(map[int64]Channel)

	var items ItemList

	for _, username := range channels {
		w.Log.Info(fmt.Sprintf("processing username: %q", username))

		peer, err := w.Client.ContactsResolveUsername(w.Context, username)
		if err != nil {
			w.Log.Error(fmt.Sprintf("unable to resolve peer for username %q: %s", username, err))
			continue
		}

		switch chat := peer.Chats[0].(type) {
		case *tg.Channel:
			var ip tg.InputPeerChannel = tg.InputPeerChannel{
				ChannelID:  chat.ID,
				AccessHash: chat.AccessHash,
			}
			peerItems, err := w.processPeer(&ip)
			if err == nil {
				items = append(items, peerItems[:]...)
			} else {
				w.Log.Error(fmt.Sprintf("processPeer failed: %s", err))
			}
		}
	}

	//
	// Sort before deduplication to favor original (non-forwarded)
	// messages
	//
	sort.Sort(items)
	before := len(items)
	items = items.dedup()
	w.Log.Info(fmt.Sprintf("removed %d items as duplicates", before-len(items)))

	//
	// Download thumbnails.  At this stage the items are ungrouped,
	// so at most one Media per item.
	//
	w.Log.Info("starting downloads")
	var success_counter, error_counter int
	for idx := range items {
		if len(items[idx].Media) > 0 && items[idx].Media[0].PendingDownload != nil {
			m := &items[idx].Media[0]
			path, err := w.downloadThumbnail(m.PendingDownload)
			if err == nil {
				success_counter++
				m.embedImageData(path)
			} else {
				error_counter++
			}
		}
	}
	w.Log.Info(fmt.Sprintf("downloads complete, %d success %d failures", success_counter, error_counter))

	//
	// Some items are supposed to be grouped together, e.g. multiple photos in an album.
	//
	items = items.group()
	sort.Sort(items)

	return items
}

func bestThumbnailSize(candidates []tg.PhotoSizeClass) (tg.PhotoSize, error) {
	//
	// https://core.telegram.org/api/files#downloading-files
	//
	var sizes []tg.PhotoSize
	for _, photoSizeClass := range candidates {
		switch photoSize := photoSizeClass.(type) {
		case *tg.PhotoSize:
			sizes = append(sizes, *photoSize)
		}
	}
	//
	// We want the smallest possible image, for now
	//
	sort.Slice(sizes, func(i, j int) bool { return sizes[i].Size < sizes[j].Size })
	if len(sizes) > 0 {
		return sizes[0], nil
	}
	return tg.PhotoSize{}, fmt.Errorf("unable to find a suitable thumbnail size")
}
