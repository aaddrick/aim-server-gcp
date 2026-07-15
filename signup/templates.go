package main

import "html/template"

// One shared layout, rendered with a page struct. Styling nods to the
// AIM-era yellow/blue without trying too hard — it has to render fine in
// anything, including old browsers people may actually use for this.
var pageTmpl = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
body { font-family: Verdana, Geneva, sans-serif; background: #4f6ec4; margin: 0; padding: 2em 1em; }
.box { max-width: 26em; margin: 0 auto; background: #ffffcc; border: 2px solid #223a8f;
       border-radius: 6px; padding: 1.5em; font-size: 14px; color: #1a1a1a; }
h1 { font-size: 1.2em; color: #223a8f; margin-top: 0; }
label { display: block; margin: 0.8em 0 0.2em; font-weight: bold; font-size: 0.85em; }
input[type=text], input[type=email], input[type=password] {
  width: 100%; box-sizing: border-box; padding: 0.4em; border: 1px solid #888; }
button { margin-top: 1.2em; padding: 0.5em 1.6em; background: #ffd837; border: 2px outset #ffe98a;
         font-weight: bold; cursor: pointer; }
.error { background: #ffdddd; border: 1px solid #cc0000; padding: 0.6em; margin-bottom: 1em; }
.hint { font-size: 0.78em; color: #555; margin: 0.15em 0 0; }
.hp { display: none; }
code { background: #fff; padding: 0 0.3em; }
.pw { font-size: 1.3em; text-align: center; margin: 0.8em 0; }
</style>
</head>
<body>
<div class="box">
<h1>{{.Title}}</h1>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}
{{if .ShowForm}}
<p>Pick a screen name, prove you own an email address, and you&rsquo;re chatting like it&rsquo;s 1999.</p>
<form method="POST" action="/signup">
  <label for="screen_name">Screen Name</label>
  <input type="text" id="screen_name" name="screen_name" maxlength="16" value="{{.ScreenName}}" required>
  <p class="hint">3&ndash;16 characters. Starts with a letter; letters, numbers, and spaces only.</p>
  <label for="email">Email Address</label>
  <input type="email" id="email" name="email" value="{{.Email}}" required>
  <p class="hint">Used to verify your signup and for password resets. One account per email is not enforced &mdash; be nice.</p>
  <div class="hp"><label for="website">Website</label><input type="text" id="website" name="website" tabindex="-1" autocomplete="off"></div>
  <button type="submit">Sign Up</button>
</form>
<p class="hint">No password to pick &mdash; you get a generated one after your email
checks out, and you can change it once you&rsquo;re in.
Already signed up? <a href="/reset">Reset your password</a>.</p>
{{end}}
{{if .Message}}<p>{{.Message}}</p>{{end}}
{{if .Verified}}
<p><b>{{.ScreenName}}</b> is ready to go. Your password:</p>
<p class="pw"><code>{{.Password}}</code></p>
<p>Write it down now &mdash; it&rsquo;s shown this once and we don&rsquo;t keep a
copy. To pick your own afterwards, use Change Password inside AIM or Pidgin,
or <a href="/reset">reset it by email</a>.</p>
<p>Point your AIM client at:</p>
<p>Host: <code>{{.Host}}</code><br>Port: <code>5190</code></p>
<p>In classic AIM: close the &ldquo;Get a Screen Name&rdquo; window, then
Setup &rarr; Connection, and set the host above. Sign on with your new
screen name and the password above.</p>
{{end}}
{{if .ShowResetForm}}
<p>Enter your screen name and we&rsquo;ll email a reset link to the address
that verified it.</p>
<form method="POST" action="/reset">
  <label for="screen_name">Screen Name</label>
  <input type="text" id="screen_name" name="screen_name" maxlength="16" value="{{.ScreenName}}" required>
  <div class="hp"><label for="website">Website</label><input type="text" id="website" name="website" tabindex="-1" autocomplete="off"></div>
  <button type="submit">Send Reset Link</button>
</form>
<p class="hint">Signed up before this site kept email records, or never here
at all? Use Change Password inside AIM or Pidgin instead.</p>
{{end}}
{{if .ShowResetConfirm}}
<form method="POST" action="/reset/confirm">
  <input type="hidden" name="token" value="{{.Token}}">
  <label for="password">New Password</label>
  <input type="password" id="password" name="password" maxlength="16" required>
  <p class="hint">4&ndash;16 characters. AIM is a retro protocol with retro security &mdash; do NOT reuse a password you use anywhere else.</p>
  <button type="submit">Change Password</button>
</form>
{{end}}
</div>
</body>
</html>
`))

type page struct {
	Title            string
	Error            string
	Message          string
	ShowForm         bool
	Verified         bool
	ShowResetForm    bool
	ShowResetConfirm bool
	ScreenName       string
	Email            string
	Password         string
	Token            string
	Host             string
}

const verifyEmailHTML = `<p>Someone (hopefully you) asked to register the AIM screen name <b>%s</b> on %s.</p>
<p><a href="%s">Confirm your screen name</a> to activate it. The link expires in %s.</p>
<p>If this wasn't you, ignore this email and the signup will evaporate on its own.</p>`

const verifyEmailText = `Someone (hopefully you) asked to register the AIM screen name %q on %s.

Confirm it by opening this link (expires in %s):

%s

If this wasn't you, ignore this email and the signup will evaporate on its own.`

const resetEmailHTML = `<p>Someone (hopefully you) asked to reset the password for the AIM screen name <b>%s</b> on %s.</p>
<p><a href="%s">Choose a new password</a>. The link expires in %s.</p>
<p>If this wasn't you, ignore this email and your password stays as it is.</p>`

const resetEmailText = `Someone (hopefully you) asked to reset the password for the AIM screen name %q on %s.

Choose a new password by opening this link (expires in %s):

%s

If this wasn't you, ignore this email and your password stays as it is.`
