# tools

This repository contains a bunch of stuff that I find useful.
Some of it may be reusable, but most of it is probably useful only to myself.

## ghreview

Provides a summary of my github activity across several github repositories during a particular year.

    $ ./ghreview 2022 RaRe-Technologies/gensim RaRe-Technologies/smart_open | tee ~/Dropbox/wiki/2022/so.html

## hijack

Hijack a github PR in order to be able to push commits to it.
Helpful for maintaining my open-source repos, e.g. [gensim](https://github.com/RaRe-Technologies/gensim) and [smart_open](https://github.com/RaRe-Technologies/smart_open).

## kp

kp works with the copy-paste buffer

read stuff into the buffer (copy):

 	$ cat file.txt | grep foo | kp
 	$ cat file.txt | grep foo | kp copy -  # more verbose version of the above
 	$ kp copy file.txt

edit the contents of the buffer (using $EDITOR):

 	$ kp edit

write the contents of the buffer to stdout (like a paste):

 	$ kp paste

## shin

SSH into an EC2 instance by its ID:

    $ shin i-12345689

Requires IDENTITY_FILE_PATH environment variable to be set to e.g. ~/.ssh/identity.pem

## workswitch

This application glues [i3](https://i3wm.org/) with various command-line utilities to make my work more productive.
Features include:

- Each i3 workspace keeps its own state:
    - Input language (Russian, English, Japanese)
    - Mouse position
    - Touchpad inhibition status
- Switch between different languages with a single command
    - For Russian, I switch the X keyboard layout (setxkmap)
    - For Japanese, I use [ibus](https://github.com/ibus/ibus) with [mozc-jp](https://github.com/google/mozc)
- Output the keyboard layout and touchpad inhibition status (for [i3blocks](https://github.com/vivien/i3blocks))
