package telegazeta

import (
	"fmt"
	"html/template"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/gotd/td/tg"
)

const templ = `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="utf-8">
		<style>
			body { font-family: Helvetica; }
			.item-list { display: grid; grid-gap: 0px; }
			.item {
				display: grid;
				grid-template-columns: 100px 200px 1000px;
				border-top: 1px solid gray;
				padding: 10px;
			}
			.item:nth-child(odd) { background-color: hsl(0, 0%, 90%); }
			.channel { font-size: large; font-weight: bold }
			/* .message p:nth-child(1) { font-weight: bold; font-size: large } */
			.placeholder {
				display: flex;
				align-items: center;
				justify-content: center;
				width: 300px;
				height: 200px;
				background-color: silver;
			}
			.thumbnails {
				display: grid;
				grid-template-columns: 325px 325px 325px;
				grid-gap: 10px;
			}
			.image-thumbnail { border-radius: 5%; }

			img {
				width: 320px;
				height: 180px;
				object-fit: contain;
				background-color: darkgrey;
			}

			/* https://stackoverflow.com/questions/44275502/overlay-text-on-image-html#44275595 */
			.container { position: relative; }
			.container p {
				position: absolute;
				bottom: 0;
				right: 0;
				color: white;
				background-color: black;
				font-size: xx-large;
				padding: 5px;
				margin: 5px;
				opacity: 50%;
			}

			.datestamp { margin: 10px; font-weight: bold; display: flex; flex-direction: column; gap: 10px; }
			.datestamp a { text-decoration: none; }
			span.time { font-size: large; font-weight: bold;  }
			span.date { font-size:  small; }
			.channel { margin-top: 10px; display: flex; flex-direction: column; gap: 10px; }
			.channel-title { font-size: small; color: gray; }
			.channel .forwarded { font-style: italic; }
			.message p { margin-top: 10px; }

			a { color: darkred; }
			a:hover { color: red; }

			blockquote {
				margin: 1em 2em;
				color: hsl(0, 0%, 25%);
				border-left: 4px solid darkred;
				padding-left: 1em;
			}
		</style>
	</head>
	<body>
		<div class='item-list'>
			{{range $index, $item := .Items}}
			<span class='item' id="item-{{$index}}" MessageID="{{$item.MessageID}}">
				<span class='datestamp'>
					<span class="time"><a href='{{$item | tgUrl}}'>{{$item.Date | formatTime}}</a></span>
					<span class="date"><a href='{{$item | tgUrl}}'>{{$item.Date | formatDate}}</a></span>
				</span>
				<span class='channel'>
				{{if $item.Forwarded}}
					<span class="domain forwarded">@{{$item.FwdFrom.Domain}}</span>
					<span class="channel-title forwarded">({{$item.FwdFrom.Title}})</span>
				{{else}}
					<span class="domain">@{{$item.Channel.Domain}}</span>
					<span class="channel-title">({{$item.Channel.Title}})</span>
				{{end}}
				</span>
				<span class='message'>
					{{$item.Text | markup}}
				{{if $item.HasWebpage}}
					<blockquote class='webpage'>
						<span class='link'><a href='{{$item.Webpage.URL}}' target='_blank'>{{$item.Webpage.Title}}</a></span>
						<span class='description'>{{$item.Webpage.Description | markup}}</span>
				{{end}}
					<span class="thumbnails">
				{{range $item.Media}}
					{{if .IsVideo}}
						{{if .Thumbnail}}
						<span class='image'>
							<span class='container'>
								<a href='{{.URL}}'>
									<img class="video-thumbnail" src='data:image/jpeg;base64, {{.ThumbnailBase64 }}' width="{{.ThumbnailWidth}}" Height="{{.ThumbnailHeight}}"></img>
								</a>
								<p>{{.Duration}}</p>
							</span>
						{{else}}
							<span class='placeholder'>
								<a href='{{.URL}}'>Video: {{.Duration}}</a>
							</span>
						{{end}}
						</span>
					{{else}}
						<span class='image'>
							<a href='{{.URL}}'><img class="image-thumbnail" src='data:image/jpeg;base64, {{.ThumbnailBase64 }}'></img></a>
						</span>
					{{end}}
				{{end}}
					</span>
				{{if $item.HasWebpage}}
					</blockquote>
				{{end}}
				</span>
			</span>
			{{end}}
		</div>
		<script>
//
// https://stackoverflow.com/questions/5353934/check-if-element-is-visible-on-screen
//
function checkVisible(elm, threshold, mode) {
	threshold = threshold || 0
	mode = mode || 'visible'

	var rect = elm.getBoundingClientRect()
	var viewHeight = Math.max(document.documentElement.clientHeight, window.innerHeight)
	var above = rect.bottom - threshold < 0
	var below = rect.top - viewHeight + threshold >= 0

	return mode === 'above' ? above : (mode === 'below' ? below : !above && !below)
}

document.addEventListener('keydown', function(event) {
	console.debug("keyCode", event.keyCode)
	var up = false
	if (event.keyCode === 74) {
		up = false
	} else if (event.keyCode === 75) {
		up = true
	} else {
		return true
	}
	var items = document.querySelectorAll(".item")

	//
	// Find the first visible item, and then scroll from it to the adjacent ones
	//
	for (let i = 0; i < items.length; ++i) {
		if (checkVisible(items[i], 50)) {
			if (up && i > 0) {
				items[i-1].scrollIntoView({behavior: 'smooth', 'block': 'end'})
				return false
			} else if (!up && i != items.length - 1) {
				items[i+1].scrollIntoView({behavior: 'smooth', 'block': 'start'})
				return false
			}
		}
	}
	return false
})
		</script>
	</body>
</html>
`

func formatDate(date time.Time) string {
	return date.Format("2 Jan")
}

func formatTime(date time.Time) string {
	return date.Format("15:04")
}

func tgUrl(item Item) template.URL {
	return template.URL(fmt.Sprintf("tg://resolve?domain=%s&post=%d", item.Channel.Domain, item.MessageID))
}

var mapping = template.FuncMap{
	"formatDate": formatDate,
	"tgUrl":      tgUrl,
	"markup":     markup,
	"formatTime": formatTime,
}

var Template = template.Must(template.New("lenta").Funcs(mapping).Parse(templ))

func markup(message string) template.HTML {
	paragraphs := strings.Split(message, "\n")
	var builder strings.Builder
	for _, p := range paragraphs {
		if len(p) > 0 {
			builder.WriteString(fmt.Sprintf("<p>%s</p>\n", p))
		}
	}
	return template.HTML(builder.String())
}

func highlightEntities(message string, entities []tg.MessageEntityClass) string {
	//
	// FIXME: debug this function, still a bit buggy for some messages
	//
	// the offsets provided by GetOffset() and friends are in UTF-16 space, so
	// we have to take our UTF-8 message, decode it to runes (implicitly as part
	// of the range function), then encode to UTF-16 chunks.
	//
	// BTW UTF-16 is a variable-width format, so a character can occupy 1 or 2
	// bytes.
	//
	var chunks [][]uint16
	for _, char := range message {
		encoded := utf16.Encode([]rune{char})
		chunks = append(chunks, encoded)
	}

	var builder strings.Builder

	type Element struct {
		Tag string
		End int
	}

	var chunkIndex int = 0
	var entityIndex int = 0
	var stack []Element
	for _, chunk := range chunks {
		//
		// First, attempt to terminate any elements that end at the current
		// chunk.  There may be more than one.
		//
		for len(stack) > 0 {
			top := len(stack) - 1
			if stack[top].End == chunkIndex {
				builder.WriteString(stack[top].Tag)
				stack = stack[:top]
			} else {
				break
			}
		}

		haveMoreEntities := entityIndex < len(entities)
		if haveMoreEntities && entities[entityIndex].GetOffset() == chunkIndex {
			end := entities[entityIndex].GetOffset() + entities[entityIndex].GetLength()
			switch e := entities[entityIndex].(type) {
			case *tg.MessageEntityBold:
				builder.WriteString(fmt.Sprintf("<strong entity='%d'>", entityIndex))
				stack = append(stack, Element{Tag: "</strong>", End: end})
			case *tg.MessageEntityItalic:
				builder.WriteString(fmt.Sprintf("<em entity='%d'>", entityIndex))
				stack = append(stack, Element{Tag: "</em>", End: end})
			case *tg.MessageEntityStrike:
				builder.WriteString(fmt.Sprintf("<s entity='%d'>", entityIndex))
				stack = append(stack, Element{Tag: "</s>", End: end})
			case *tg.MessageEntityTextURL:
				builder.WriteString(fmt.Sprintf("<a entity='%d' href='%s' target='_blank'>", entityIndex, e.URL))
				stack = append(stack, Element{Tag: "</a>", End: end})
			case *tg.MessageEntityURL:
				//
				// Argh, now we have to work out what the URL is from the stuff
				// we've just encoded...
				//
				var urlChunks []uint16
				for i := 0; i < e.Length; i++ {
					if e.Offset+i >= len(chunks) {
						break
					}
					urlChunks = append(urlChunks, chunks[e.Offset+i]...)
				}
				var url []rune = utf16.Decode(urlChunks)
				builder.WriteString(fmt.Sprintf("<a entity='%d' href='%s' target='_blank'>", entityIndex, string(url)))
				stack = append(stack, Element{Tag: "</a>", End: end})
			}

			entityIndex += 1
		}

		var decodedChunk []rune = utf16.Decode(chunk)
		builder.WriteString(string(decodedChunk))

		chunkIndex += len(chunk)
	}

	//
	// Ensure we've closed all the tags that we've opened, just to be safe.
	//
	for len(stack) > 0 {
		top := len(stack) - 1
		builder.WriteString(stack[top].Tag)
		stack = stack[:top]
	}

	return builder.String()
}
