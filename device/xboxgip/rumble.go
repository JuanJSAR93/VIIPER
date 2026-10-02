package xboxgip

import (
	"sync"
	"time"
)

const (
	directMotorPayloadSize = 9
	directMotorMaximum     = 100
)

const (
	motorRightVibration byte = 1 << iota
	motorLeftVibration
	motorRightImpulse
	motorLeftImpulse
	motorAll = motorRightVibration | motorLeftVibration |
		motorRightImpulse | motorLeftImpulse
)

// directMotor is the exact nine-byte MS-GIPUSB Direct Motor body. Duration
// and Delay use 10 ms units and Repeat is the number of additional plays.
// It stays private because the VIIPER stream deliberately retains its
// established two-motor feedback contract.
type directMotor struct {
	enabled                       byte
	leftImpulse, rightImpulse     byte
	leftVibration, rightVibration byte
	duration, delay, repeat       byte
}

func decodeDirectMotor(payload []byte) (directMotor, bool) {
	if len(payload) != directMotorPayloadSize || payload[0] != 0 ||
		payload[1]&^motorAll != 0 || payload[2] > directMotorMaximum ||
		payload[3] > directMotorMaximum || payload[4] > directMotorMaximum ||
		payload[5] > directMotorMaximum {
		return directMotor{}, false
	}
	return directMotor{
		enabled:        payload[1],
		leftImpulse:    payload[2],
		rightImpulse:   payload[3],
		leftVibration:  payload[4],
		rightVibration: payload[5],
		duration:       payload[6],
		delay:          payload[7],
		repeat:         payload[8],
	}, true
}

func (command directMotor) state() RumbleState {
	var state RumbleState
	if command.enabled&motorLeftVibration != 0 {
		state.LeftMotor = command.leftVibration
	}
	if command.enabled&motorRightVibration != 0 {
		state.RightMotor = command.rightVibration
	}
	// The established VIIPER feedback stream exposes two motors. Preserve
	// trigger-impulse feedback by folding each impulse into its corresponding
	// main motor, without ever treating a masked-off value as active.
	if command.enabled&motorLeftImpulse != 0 && command.leftImpulse > state.LeftMotor {
		state.LeftMotor = command.leftImpulse
	}
	if command.enabled&motorRightImpulse != 0 && command.rightImpulse > state.RightMotor {
		state.RightMotor = command.rightImpulse
	}
	return state
}

func (command directMotor) active() bool {
	state := command.state()
	return command.duration != 0 && (state.LeftMotor != 0 || state.RightMotor != 0)
}

func (command directMotor) initialDelay() time.Duration {
	return time.Duration(command.delay) * 10 * time.Millisecond
}

func (command directMotor) activeDuration() time.Duration {
	plays := time.Duration(command.repeat) + 1
	return time.Duration(command.duration) * 10 * time.Millisecond * plays
}

// rumbleScheduler is one retained executor per virtual GIP controller. A new
// command replaces the previous program, so stale timers can never restart
// motors after a cancellation, lifecycle transition, or newer command.
//
// The GIP Direct Motor body defines a delay, duration and repeat count but no
// separate off interval between repeats. VIIPER therefore applies the delay
// once and represents repeat as one contiguous active interval. This avoids
// inventing pulses that were not present on the wire and keeps a duration of
// N * (repeat + 1) ten-millisecond ticks.
type rumbleScheduler struct {
	mu       sync.Mutex
	timer    *time.Timer
	epoch    uint64
	active   bool
	state    RumbleState
	callback func(RumbleState)
}

func (scheduler *rumbleScheduler) SetCallback(callback func(RumbleState)) {
	scheduler.mu.Lock()
	scheduler.callback = callback
	scheduler.mu.Unlock()
}

func (scheduler *rumbleScheduler) Submit(command directMotor) {
	state := command.state()

	scheduler.mu.Lock()
	if scheduler.timer != nil {
		scheduler.timer.Stop()
		scheduler.timer = nil
	}
	scheduler.epoch++
	epoch := scheduler.epoch
	previous := scheduler.state
	scheduler.active = false
	scheduler.state = RumbleState{}

	if !command.active() {
		callback := scheduler.callback
		scheduler.mu.Unlock()
		if previous != (RumbleState{}) && callback != nil {
			callback(RumbleState{})
		}
		return
	}

	scheduler.active = true
	if delay := command.initialDelay(); delay > 0 {
		scheduler.timer = time.AfterFunc(delay, func() {
			scheduler.start(epoch, state, command.activeDuration())
		})
		callback := scheduler.callback
		scheduler.mu.Unlock()
		if previous != (RumbleState{}) && callback != nil {
			callback(RumbleState{})
		}
		return
	}

	scheduler.state = state
	scheduler.timer = time.AfterFunc(command.activeDuration(), func() {
		scheduler.finish(epoch)
	})
	callback := scheduler.callback
	scheduler.mu.Unlock()
	if callback != nil {
		callback(state)
	}
}

func (scheduler *rumbleScheduler) start(epoch uint64, state RumbleState, duration time.Duration) {
	scheduler.mu.Lock()
	if !scheduler.active || scheduler.epoch != epoch {
		scheduler.mu.Unlock()
		return
	}
	scheduler.state = state
	scheduler.timer = time.AfterFunc(duration, func() { scheduler.finish(epoch) })
	callback := scheduler.callback
	scheduler.mu.Unlock()
	if callback != nil {
		callback(state)
	}
}

func (scheduler *rumbleScheduler) finish(epoch uint64) {
	scheduler.mu.Lock()
	if !scheduler.active || scheduler.epoch != epoch {
		scheduler.mu.Unlock()
		return
	}
	scheduler.active = false
	scheduler.timer = nil
	scheduler.state = RumbleState{}
	callback := scheduler.callback
	scheduler.mu.Unlock()
	if callback != nil {
		callback(RumbleState{})
	}
}

// Stop clears the current state immediately. It is used by GIP STOP, OFF,
// QUIESCE and RESET transitions as well as a zero-duration Direct Motor body.
func (scheduler *rumbleScheduler) Stop() {
	scheduler.mu.Lock()
	if scheduler.timer != nil {
		scheduler.timer.Stop()
		scheduler.timer = nil
	}
	scheduler.epoch++
	shouldEmit := scheduler.active || scheduler.state != (RumbleState{})
	scheduler.active = false
	scheduler.state = RumbleState{}
	callback := scheduler.callback
	scheduler.mu.Unlock()
	if shouldEmit && callback != nil {
		callback(RumbleState{})
	}
}
