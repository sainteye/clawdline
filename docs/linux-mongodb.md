# MongoDB for Linux agent tests

This is the measured setup for a single-machine development database on Ubuntu 24.04 amd64. It is
not the Cloud control plane's production database. The service listens only on loopback, has no
database users, and exists so an Agent on the same host can run replica-set features such as
transactions and change streams.

## Kernel gate first

MongoDB 8.0 refuses to start when `uname -r` begins with `6.19` or a later major/minor release.
Check the running kernel before installing the package:

```sh
uname -r
```

Do not infer compatibility from a vendor's upstream backport level. On the Linux test host measured
on 2026-09-28, an AWS kernel whose package was based on upstream 7.0.14 still reported
`7.0.0-1013-aws`; MongoDB 8.0.32 saw `7.0` and stopped before opening its log file. Ubuntu's
`6.8.0-142-generic` kernel started MongoDB successfully.

An Ubuntu cloud image may set `GRUB_FORCE_PARTUUID`, which deliberately generates an initrdless
boot entry. A generic kernel needs its initramfs to find an NVMe root filesystem. Before selecting a
generic kernel on such an image:

1. Install an exact, supported image and its modules rather than a rolling kernel meta-package.
2. Add a later file under `/etc/default/grub.d` that sets `GRUB_FORCE_PARTUUID=` and selects the
   generic kernel by its GRUB submenu and menu-entry IDs.
3. Run `update-grub` and refuse the reboot if its output still says it will attempt an initrdless
   boot.
4. Confirm the selected generic entry in `/boot/grub/grub.cfg` contains an `initrd` line for that
   exact image.
5. Keep the existing cloud kernel installed as a recovery entry, reboot, then prove the running
   value with `uname -r`.

The measured exact-image install was:

```sh
sudo apt-get install -y \
  linux-image-6.8.0-142-generic \
  linux-modules-extra-6.8.0-142-generic
```

List the IDs that step 2 needs instead of guessing their text or numeric positions:

```sh
grep -E "^submenu |^[[:space:]]*menuentry " /boot/grub/grub.cfg
```

The measured host uses this override shape; the IDs and kernel version must come from the host being
changed:

```sh
GRUB_FORCE_PARTUUID=
GRUB_DEFAULT="advanced-submenu-id>generic-kernel-entry-id"
```

## Install MongoDB 8.0 Community

Use MongoDB's signed Ubuntu 24.04 repository:

```sh
sudo apt-get update
sudo apt-get install -y gnupg curl
curl -fsSL https://pgp.mongodb.com/server-8.0.asc \
  | sudo gpg --dearmor --yes -o /usr/share/keyrings/mongodb-server-8.0.gpg
printf '%s\n' \
  'deb [ arch=amd64,arm64 signed-by=/usr/share/keyrings/mongodb-server-8.0.gpg ] https://repo.mongodb.org/apt/ubuntu noble/mongodb-org/8.0 multiverse' \
  | sudo tee /etc/apt/sources.list.d/mongodb-org-8.0.list >/dev/null
sudo apt-get update
sudo apt-get install -y mongodb-org
```

Keep the package defaults `storage.dbPath: /var/lib/mongodb`, `net.port: 27017`, and
`net.bindIp: 127.0.0.1`. Add the replica-set setting to `/etc/mongod.conf`:

```yaml
replication:
  replSetName: rs0
```

Then enable the service and initialize it once:

```sh
sudo systemctl enable --now mongod
mongosh --quiet --eval \
  'rs.initiate({_id:"rs0",members:[{_id:0,host:"127.0.0.1:27017"}]})'
```

Wait until `db.hello().isWritablePrimary` is true before running a test. Agents on this host use:

```sh
export MONGO_URI='mongodb://127.0.0.1:27017/?replicaSet=rs0&directConnection=true'
```

Do not open port 27017 or change `bindIp` for convenience. Loopback is the security boundary for
this unauthenticated development service.

## Verify before depending on it

The check is more than a successful `ping`: write a sentinel, commit a transaction, restart the
service, read both records back, and drop the smoke database. Also prove the systemd and listener
boundaries:

```sh
systemctl is-enabled mongod
systemctl is-active mongod
mongosh --quiet --eval \
  'const h=db.hello(); printjson({setName:h.setName,primary:h.primary,writable:h.isWritablePrimary})'
ss -ltnp | grep -E '127\.0\.0\.1:27017[[:space:]]'
```

The 2026-09-28 verification used MongoDB 8.0.32 and passed all of the following:

- booted `6.8.0-142-generic` with its initramfs;
- started `mongod` automatically after that reboot;
- elected the single `rs0` member as writable primary;
- committed a transaction;
- retained the sentinel and committed row across `systemctl restart mongod`;
- listened on `127.0.0.1:27017` and not on wildcard IPv4 or IPv6 addresses;
- dropped the smoke database after the readback.

## Disk placement, cleanup and expansion

The measured host kept MongoDB on the root filesystem. After installation and verification,
`/var/lib/mongodb` used 202 MB and the 30 GB root filesystem had 16 GB available. A separate 20 GB
Clawdline data filesystem had 6.9 GB available, so moving MongoDB there would have reduced the
headroom and weakened the existing `0700` boundary around Clawdline state. No volume expansion was
needed.

When space is tight, measure first:

```sh
df -h / /boot /var/lib/clawdline
sudo du -xhd1 /var/lib/mongodb /var/tmp /tmp 2>/dev/null
sudo journalctl --disk-usage
```

Safe first candidates are the APT download cache and reproducible package/build caches. Do not
delete projects, session state, databases, or a young `/var/tmp` tree merely because they are
large. On the measured host, no `/var/tmp` content was older than 30 days, and Clawdline projects
were the largest part of its data volume, so neither was removed.

If growth eventually requires more space, expand the existing block volume and then the partition
and filesystem; do not try to shrink or replace it in place. Take a recoverable snapshot first,
identify the mounted device with `findmnt` and `lsblk`, and use the filesystem-specific grow tool.
For the measured ext4-on-EBS layout that means growing the EBS volume, running `growpart` for its
partition, then `resize2fs`, with `df` proving the new size. A database migration to another volume
is a separate maintenance operation: stop `mongod`, copy and verify ownership and bytes, change
`storage.dbPath`, and keep the original data until the restarted replica set passes the persistence
check.
