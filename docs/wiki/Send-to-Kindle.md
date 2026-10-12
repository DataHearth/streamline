# Send to Kindle

Streamline can email an ebook to your e-reader. Amazon's Send to Kindle by email accepts it at your Kindle's address, and PocketBook's equivalent works the same way.

## Setup

1. Add an `smtp` section to the config file (it is file-only and needs a restart; see [Configuration Reference](Configuration-Reference)):

```yaml
smtp:
  host: smtp.example.com
  port: 587
  tls: starttls              # starttls | implicit | none
  username: streamline
  password_file: /run/secrets/smtp
  from: streamline@example.com
  max_message_size_mb: 25
```

2. Approve the `from` address in Amazon's **Content & Devices > Preferences > Personal Document Settings > Approved Personal Document E-mail List**. Amazon drops mail from any other sender without telling you.
3. Copy your Kindle's address (`name_xxxx@kindle.com`) into the **Send to device** card on the Account page.

A **Send** button then appears on a book's page, and on a series page for all its volumes. The button is disabled, with the reason as its tooltip, when the book cannot be sent.

## Which files can be sent

| Format | Sendable |
| --- | --- |
| EPUB | Yes |
| PDF | Yes, when the book has no EPUB |
| CBZ, CBR | No |
| AZW3, MOBI | No |

A book holding both EPUB and PDF sends the EPUB. Streamline does not convert anything, and Amazon no longer takes CBZ, AZW3 or MOBI by email. Read those through the [OPDS catalogue](Music-and-Books) in a reader app instead. Converting CBZ to PDF is on the [Roadmap](Roadmap).

## Size limits

`smtp.max_message_size_mb` (default 25) caps the raw attachment bytes in one email, before base64 encoding inflates them by about a third. Amazon accepts up to 50 MB per email, but many mail relays refuse messages well under that, so keep the value at or below what your relay allows. A file over the limit is not sendable and the reason names both sizes. An email also carries at most 25 attachments.

## Sending a series

A series send packs the hydrated, sendable volumes into as few emails as fit under the size limit, in volume order. The request answers at once with how many emails and volumes were queued and which volumes were skipped, with the reason for each (a CBZ volume, a missing file, a file over the limit).

The emails are sent in the background. If one fails, nothing is shown in the interface: look for a `WARN` line from the book service in the logs (see [Observability and Logging](Observability-and-Logging)), which carries the SMTP server's reply. Sending a single book reports its failure on screen.

## Security

`tls: none` only loads when `smtp.host` is a loopback address, so credentials never cross a network in clear. With `starttls`, a server that does not offer STARTTLS makes the send fail before authentication instead of falling back to plain text.
