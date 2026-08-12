# Learnings

- The verifier needs the address-family branch to stay in one function;
  splitting it into a helper loses the range it proved on the packet
  pointer.
