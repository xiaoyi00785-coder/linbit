# Android Passkey login — isolated verification

## Scope

This slice connects the existing anonymous API contracts
`POST /auth/passkey/login/begin` and `/auth/passkey/login/finish`. The production
bridge uses Android's public `androidx.credentials.CredentialManager` and returns
`PublicKeyCredential.authenticationResponseJson` to the existing server verifier.
Only the server response reaches `TokenStore.save()` after the current ViewModel
login generation and session checks.

Registration begin/finish, credential listing, deletion/second verification are
NOT part of this slice. No production security, credential, funds or alert write
is performed by tests. No real credential or production token is used.

## Reproduce locally

```sh
cd mobile/android
./gradlew :app:compileDebugKotlin :app:compileDebugUnitTestKotlin :app:lintDebug
./gradlew :app:testDebugUnitTest --tests 'click.cowa.linbit.feature.auth.Passkey*'
./gradlew :app:testDebugUnitTest
```

Host tests inject `PasskeyCredentialBridge` and explicitly route loopback API
responses. Screenshots in `app/build/reports/passkey-shots/` cover a narrow,
large-font light login and a dark login with recoverable unsupported/no-credential
states. These are app host screenshots, NOT screenshots of an Android system
credential sheet. Native UI coverage is not equivalent to a device Passkey ceremony.
The owner performs final device ceremony testing; that is not an APK packaging
prerequisite, but the deployment prerequisite below remains required for a working
real Android Passkey login.

The focused tests cover real Retrofit request serialization and the official token
store path, duplicate starts, cancel/unsupported/no-credential/failure recovery,
native deferred cancellation, session changes during the native call, and a
loopback HTTP finish already in flight when the session changes. The finish
response hold is test-only, bounded at 5 seconds, and releases in `finally`.

## Production deployment prerequisite (READ-ONLY finding)

On 2026-11-06, a read-only public check returned HTTP 404 for
`https://api.cowa.click/.well-known/assetlinks.json`; the other checked
`cowa.click` host did not provide a valid associated document during the check.
This is not proof that Android can or cannot log in at a later deployment time.

Before final device validation, the release operator must configure the **actual**
RP/origin host consistently with the backend RP configuration. Do not silently
change `WEBAUTHN_RP_ID`, origins, production domains or feature flags to accommodate
the app. For the actual Android RP host, publish a valid `assetlinks.json` relation
similar to this template, replacing the host and certificate with verified values:

```json
[{
  "relation": ["delegate_permission/common.get_login_creds"],
  "target": {
    "namespace": "android_app",
    "package_name": "click.cowa.linbit",
    "sha256_cert_fingerprints": [
      "RELEASE_CERT_SHA256_CONFIRMED_BY_RELEASE_OWNER"
    ]
  }
}]
```

The signing-certificate owner must confirm the release fingerprint from the
allowable public certificate value. Do not read, print, export or replace a private
signing key. Current native compilation and mock tests do NOT validate this RP
association or a real authenticator.


## Registration and security-center slice

`SecurityHost` now opens `PasskeySecurityScreen` instead of a placeholder Toast.
Production registration uses `CreatePublicKeyCredentialRequest`,
`CredentialManager.createCredential`, and validates the public creation response
shape. The authenticated begin sends exactly one of `totpCode` (enabled TOTP) or
`code` (other security-verification method). After finish, the client requires an
actual `GET /account/passkeys` readback before updating the host list and showing
registration success. `DELETE /account/passkeys/{id}` is not exposed in this batch.

`PasskeyRegistrationStateTest` and `PasskeyRegistrationVisualTest` use only
`http://127.0.0.1` with fake short-lived session/token strings and explicit native
fakes. They test precise TOTP security-code requests, duplicate clicks, cancelled
and failed creation (zero finish), activity disposal/session generations, a delayed
finish after a session change, and list readback. The two PNGs prefixed
`passkey-register-` in `app/build/reports/passkey-shots/` are actual application
`SecurityHost` pixels, not system-sheet or device ceremony evidence. They cover
320dp/large font/light and 360dp/dark host recovery layouts. The app's error labels
never claim that Android validated a real new credential.

The deployment template above applies to both login and registration through the
actual release RP origin. Existing known-missing association documentation was not
reprobed for this batch; the missing association remains a release usability
prerequisite, not a reason to discard these implementation/test results.

## Deletion slice (frontend final-review, not backend 2FA)

`DELETE /account/passkeys/{id}` is exposed only from the real server-returned
list. The final-review Sheet explicitly explains that the deleted credential will
no longer login to this account. Cancellation before request makes no DELETE;
in-flight requests disable duplicate calls and are not misleadingly called
"cancelled". Errors retain the review target for retry. A 204 response is followed
by a current-session list readback before the UI updates. A session change
invalidates the response/readback, so a stale deletion cannot overwrite the new
account's local list. The public DELETE request contains no second-factor field;
the Sheet says this is frontend confirmation only. Deletion-specific backend
2FA requires a separately reviewed API/policy contract, not a client invention.

`PasskeyDeletionStateTest` covers cancellation, duplicate confirm, readback,
failure/retry, and a held loopback DELETE crossing a session switch. The
`passkey-delete-dialog-*.png` captures are independent production `LbSheet` window
canvases through its real Window hook (not system credential windows or device
screen captures). No remote real DELETE was sent.
