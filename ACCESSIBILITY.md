# Accessibility statement

The backend is the API behind the energy audit dashboard for small businesses. It has no screens of its own, so what it can do for accessibility is to make the job of the dashboard easier and to keep its own documentation usable. This page says what is promised, where the limits are and how to report a barrier.

## Commitment

- Every failure has a stable `code` and a plain English `message`, and a field that failed validation is named in `fields` with a code. The dashboard can show its own text, in the form that works for the person, and never has to read out a code.
- Every JSON response carries data and text and never layout, color or markup, so the dashboard decides how everything is presented.
- A response of 429 has a `Retry-After` header and `retryAfterSeconds`, so the dashboard can tell the person when to try again.
- The report PDF writes every number as text next to its bar, so the figures are never only a drawing.
- The links in the Markdown pages are checked by CI every week.

## Supported environments

- The API is used over HTTPS by any client that can send JSON.
- The contract is the file [openapi.yaml](openapi.yaml), and there is no hosted reference site.
- The Markdown pages of this repository are read on GitHub or in any text editor.

## Known limitations

- Nothing here has been audited for accessibility, and no conformance claim is made for any standard such as WCAG. The API has no interface to audit.
- The report PDF is drawn with a library that does not write PDF tags, so a screen reader may read its text in an order that is hard to follow. The dashboard should offer the same figures on a screen as text.
- Error messages are in English. The text of tips and of the report PDF is in Indonesian. A dashboard that serves people in another language has to map the codes to its own text.

## Report a barrier

Open an issue with the bug report form and say that it is an accessibility barrier. Describe what you were trying to do, where it went wrong and which tools you use, such as a browser and a screen reader. The form applies the `bug` label, and this repository does not promise an accessibility label or a reply time for these reports.

A barrier in the dashboard itself belongs to the [frontend repository](https://github.com/aethera-deeptech/frontend) and not to this one.
