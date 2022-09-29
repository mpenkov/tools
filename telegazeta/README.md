# Telegazeta

This bot helps read news from multiple Telegram channels.
You give it a list of channels in a text file, and it gathers messages from those channels, sorts them by time and outputs them as static HTML, including images.
It's written in Golang, so you need that to first build the binaries:

    $ go mod tidy
    $ go build

For authentication and authorization, you need two tokens from my.telegram.org, the app ID and app hash.
These tokens are specific to your personal Telegram account, so keep them secret.

    $ export APP_ID=...
    $ export APP_HASH=...

Finally, run the bot:

    ./telegazeta -phone "..." -channels channels.txt 2> stderr | tee $(date "+%Y%m%d.html")

- The phone number is what you use to log into telegram
- The channels file contains channel names, one per line (without the leading @ mark)

The very first time you run this, you will be asked to approve the application by entering a code sent to your Telegram account.
Subsequent runs will not require this step.

Open the generated HTML file in your browser.
Use j and k (vim forever) to scroll up and down.

# Demo

See [here](https://raw.githubusercontent.com/mpenkov/tools/master/telegazeta/testdata/demo.html).
It was generated using the following command:

    $ cat testdata/channels.txt
    bbcworldnews
    nytimes
    reutersworldchannel
    worldnews
    $ ./telegazeta -phone "..." -channels testdata/channels.txt -hours 8 2> stderr | tee testdata/demo.html

