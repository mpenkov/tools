# kp

kp works with the copy-paste buffer

read stuff into the buffer (copy):

 	$ cat file.txt | grep foo | kp copy -
 	$ kp copy file.txt

edit the contents of the buffer (using $EDITOR):

 	$ kp edit

write the contents of the buffer to stdout (like a paste):

 	$ kp paste
