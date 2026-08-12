# durdur — private notes

Policy-map versioning: bump the version byte and add a loader path, never
edit the existing map shape. A running host reloads the policy live, so a
bad layout takes the firewall down.
