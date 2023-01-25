# kp

kp works with the copy-paste buffer.
It's essentially a clone of [xsel](https://linux.die.net/man/1/xsel) that I've tweaked to satisfy several personal use cases.
I use it as a bridge between my command-line working environment and UI apps (mostly, the browser).

## copy

read stuff into the buffer (copy):

 	$ cat file.txt | grep foo | kp
 	$ cat file.txt | grep foo | kp copy -  # more verbose version of the above
 	$ kp copy file.txt

## paste

write the contents of the buffer to stdout (like a paste):

 	$ kp paste  # in case C-S-v, C-S-Insert and friends refuse to work for some reason.

I frequently use `kp paste` from within vim, because that pastes the content as-is, without vim messing up the indentation.
I find this to be a quicker alternative then entering vim's paste mode, pasting, and then exiting the paste mode.

## edit

Often, I'll paste stuff into a UI application only to have to edit what I just pasted.
This is slow, so instead I first edit the contents of the buffer (using $EDITOR):

 	$ kp edit

## tmux

write the contents of the current [tmux](https://github.com/tmux/tmux/wiki) pane to the buffer, and then edit the buffer (using $EDITOR):

    $ kp tmux

This allows you to easily share snippets of output from your console with others.
I find this to be much quicker than using tmux's copy-mode-vi (or copy-mode depending on the local tmux config).
`kp tmux` instantly puts the whole pane into vim, and from there I can edit at the speed of thought.
