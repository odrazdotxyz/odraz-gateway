# Drive manifest (draft, version 1)

A drive can carry a small settings file that says how it is organised, so every client that opens the drive
(the web page, a desktop app, a phone app) shows the same thing. It lives **in the drive itself**: no database,
no server of ours, and it moves with the data.

```
/.odraz/manifest.json
```

```json
{
  "version": 1,
  "sections": {
    "photos": { "folder": "/Pictures/Camera Roll", "hideInFiles": true }
  }
}
```

| Field | Meaning |
|---|---|
| `version` | Format version. This document is version 1. |
| `sections.<name>` | One entry per section of the app. Defined: `photos`. Unknown names must be kept as they are. |
| `folder` | Absolute path of a folder in this drive (not `/`, not under `/.odraz`). |
| `hideInFiles` | If true, plain file views do not list that folder. It is a view setting: the folder is never moved or renamed, and a "show hidden" switch reveals it. |

## Rules for clients

1. **The reserved folder.** Everything the system writes into a drive goes under `/.odraz/` (the manifest, and
   later thumbnails or archive pointers). Nothing else of ours may be written elsewhere in the user's tree.
2. **Hidden by default.** Plain file views leave out `/.odraz` and every entry whose name starts with `.`.
3. **No manifest is normal.** Behave as if there were no sections. Never create a manifest just to browse.
4. **Never overwrite what you cannot read.** If the file is not valid JSON, is bigger than 64 KiB, or has a
   `version` higher than you know, read it as empty and **refuse to write**; tell the user instead. An older
   client must not destroy a newer client's data.
5. **Removing the last section deletes the manifest** (and `/.odraz` if it is then empty), so an unset drive is
   clean again.
6. **Only the owner's drives.** A read-only drive cannot hold a manifest; clients use the defaults for it.

## The Photos section

The user chooses the folder (nothing is guessed or moved). Photos shows only the pictures and videos in that
folder and everything below it, as one list across every drive that has a `photos` section; other files in the
folder are not part of Photos. Because `hideInFiles` removes the folder from the plain file views, a client
should give a way to reveal hidden folders there ("Show hidden"), so those other files stay reachable.
Thumbnails come from the server where it has them (`files.Preview` in the SDK).

## Reference implementation

`pkg/manifest` in the demo (about 100 lines, depends only on the SDK's `files.FS`). It is meant to move
to an optional package once a second client needs it; the SDK core stays storage-only.
