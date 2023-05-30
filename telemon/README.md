This utility reads messages from an IMAP inbox and forwards them to a Telegram channel.

By default, it reads configuration from gitignore/config.json.
The config is JSON that looks like this:

    {
      "IMAP": {
        "Folder": "INBOX",
        "Host": "imap.fastmail.com",
        "MaxCount": 10,
        "Password": "secret",
        "Port": 993,
        "SubjectFilters": [
          "regular expression 1",
          "regular expression 2",
          "regular expression 3",
        ],
        "User": "user@fastmail.com"
      },
      "Telegram": {
        "APIHash": "secret",
        "APIID": 0123456789,
        "PhoneNumber": "secret",
        "ChannelName": "secret"
      },
      "TempDir": "/tmp/telemon"
    }

The IMAP section is mostly self-explanatory.
The utility reads up to MaxCount most recent message subjects and matches them against SubjectFilters.
It fetches the bodies of matching messages and sends them to Telegram.ChannelName.
That channel must already exist and belong to the Telegram user identified by the PhoneNumber.

The TempDir keeps a bunch of temporary data for the utility, including the Telegram session and IMAP message tracking.
The utility tracks the most recently seen messages from the IMAP inbox, and ignores messages it has already seen.
This means you may call the utility in a tight loop - it will never send the same message twice.
