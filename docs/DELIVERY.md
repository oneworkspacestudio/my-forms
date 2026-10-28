# Preview 1 delivery

My Forms 0.1.0-preview.1 is published with eight digest-verified assets and installed
through One Workspace's normal protected update flow on the requesting Windows PC.
The public app source/release is in oneworkspacestudio/my-forms; integration source
is in oneworkspacestudio/one-workspace. Actions remain disabled.

The locally built Workspace98 integration includes an upgrade migration for the
existing Drive recovery service account and a Forms storage readiness completion
gate. The native regression fixture reproduced HTTP403 from the missing source,
then verified automatic permission repair and real CRUD/submission/export/upload.

The public Workspace installer release is separately pending the permanent Android
signing setup required by its repository policy. This preview does not include a
public standalone Workspace installer. See feature-parity.md for remaining scope.

Windows packaging check: x64 PE; kernel32.dll imports only; independent trimmed
build produces identical bytes. Native fixture processes/data removed after checks.
No clean-host, reboot, browser visual/accessibility or complete feature-parity claim.
