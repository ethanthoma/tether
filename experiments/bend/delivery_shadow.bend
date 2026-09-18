import Base
import ./dispatch.bend as Policy

def encode(state: Policy.DeliveryState) -> String:
  match state:
    case Policy.Ready{confirmed}:
      "0" ++ Nat.show(confirmed)
    case Policy.Failed{confirmed}:
      "1" ++ Nat.show(confirmed)

def outcomes(+confirmed: Nat) -> String:
  first = encode(Policy.delivery_step(False{}, Policy.Ready{confirmed}, False{})) ++ encode(Policy.delivery_step(True{}, Policy.Ready{confirmed}, False{}))
  second = encode(Policy.delivery_step(False{}, Policy.Failed{confirmed}, False{})) ++ encode(Policy.delivery_step(True{}, Policy.Failed{confirmed}, False{}))
  third = encode(Policy.delivery_step(False{}, Policy.Ready{confirmed}, True{})) ++ encode(Policy.delivery_step(True{}, Policy.Ready{confirmed}, True{}))
  fourth = encode(Policy.delivery_step(False{}, Policy.Failed{confirmed}, True{})) ++ encode(Policy.delivery_step(True{}, Policy.Failed{confirmed}, True{}))
  first ++ second ++ third ++ fourth

def main() -> IO(Unit):
  first = outcomes(0n) ++ outcomes(1n)
  second = outcomes(2n) ++ outcomes(3n)
  third = outcomes(4n) ++ outcomes(5n)
  IO.print("tether-bend-delivery-v1:" ++ first ++ second ++ third)
