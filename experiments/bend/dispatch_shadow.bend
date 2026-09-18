import Base
import ./dispatch.bend as Policy

def candidates(+quiet: Bool, +fired: Nat) -> String:
  first = Nat.show(Policy.batch_count(quiet, fired, 0n)) ++ Nat.show(Policy.batch_count(quiet, fired, 1n))
  second = Nat.show(Policy.batch_count(quiet, fired, 2n)) ++ Nat.show(Policy.batch_count(quiet, fired, 3n))
  third = Nat.show(Policy.batch_count(quiet, fired, 4n)) ++ Nat.show(Policy.batch_count(quiet, fired, 5n))
  first ++ second ++ third

def counts(+quiet: Bool) -> String:
  first = candidates(quiet, 0n) ++ candidates(quiet, 1n)
  second = candidates(quiet, 2n) ++ candidates(quiet, 3n)
  third = candidates(quiet, 4n) ++ candidates(quiet, 5n)
  first ++ second ++ third

def main() -> IO(Unit):
  IO.print("tether-bend-dispatch-v1:" ++ counts(False{}) ++ counts(True{}))
