# tools

This repository contains a bunch of stuff that I find useful.
Some of it may be reusable, but most of it is probably useful only to myself.

To install any of these tools:

    $ go install github.com/mpenkov/tools/<toolname>@latest

where <toolname> is one of the below.

## ghreview

Provides a summary of my github activity across several github repositories during a particular year.

    $ ./ghreview 2022 RaRe-Technologies/gensim RaRe-Technologies/smart_open | tee ~/Dropbox/wiki/2022/so.html

## hijack

Hijack a github PR in order to be able to push commits to it.
Helpful for maintaining my open-source repos, e.g. [gensim](https://github.com/RaRe-Technologies/gensim) and [smart_open](https://github.com/RaRe-Technologies/smart_open).

## kot

Like cat, but with auto-completion for S3 and HTTP (TODO).
To install, first run:

     COMP_INSTALL=1 kot

and answer "yes" to the prompt.

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

Offers a helpful `-register` flag that updates `~/.ssh/config` to simplify reconnecting to the instance, using `scp`, etc.
For example:

    $ shin -register -alias worker -dnc i-12345689
    # <shin>
    Host worker
            Hostname 1.2.3.4
            User ubuntu
            IdentityFile /home/misha/.ssh/identity.pem
            StrictHostKeyChecking=accept-new
    # </shin>

The "-dnc" flag prevents shin from making the initial SSH connection.
The "-alias" flag specifies what to call the new host.
It will default to the instance ID, or the instance's name, if it has a "Name" tag.
The standard output shows what was added to your ~/.ssh/config file.
You can now do this:

    $ ssh worker ls
    misha@cabron:~/git/tools/shin$ ssh worker ls -lh /home
    total 4.0K
    drwxr-xr-x 17 ubuntu ubuntu 4.0K Feb 17  2022 ubuntu

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
