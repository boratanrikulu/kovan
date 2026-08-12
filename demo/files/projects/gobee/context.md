# gobee — private notes

The verifier is the oracle. When generated code is rejected, reduce to the
smallest failing program and read the verifier log top-down; the first lost
range is the bug, not the last.
