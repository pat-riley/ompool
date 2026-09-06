// ompool instrument - Bitcoin sonification (melodic IDM pass)
//
// `ompool audio` runs this file in sclang automatically; the copy it runs is
// written to ~/.cache/ompool/ompool.scd and its output to instrument.log there.
// To work on it in SCIDE instead: `ompool instrument > my.scd`, open that,
// evaluate the whole block (cursor inside the outer parens, Ctrl+Enter), and
// start `ompool audio` - it sees SCIDE holding the OSC port and feeds it rather
// than launching a second sclang. Point OMPOOL_INSTRUMENT at your file to have
// ompool launch your version.
// Re-evaluating is safe: it stops the old sequencer and rebuilds everything.
// Ctrl+. stops all sound.
//
// DATA -> MUSIC MAP
//   EVERY transaction                     -> one plucked "ping" (on ompool's grid, or instantly)
//   tx fee rate vs. market rate           -> pitch across three octaves: cheap = low, market rate = middle, urgent = high;
//                                            also brightness (urgent = bright) and pan (cheap = left, urgent = right)
//   tx value (sats)                       -> loudness, ring length and reverb (big amounts ring in more space);
//                                            >= 1 BTC adds a deep bell an octave below
//   a tx paying > 3x market               -> snare ratchet (an "urgent" glitch)
//   new block                             -> gong + chord change (steps the progression)
//   block tx count                        -> gong brightness
//   fastestFee (network fee pressure)     -> "tension": pad brightness, 9ths fade in, extra kick, ghost notes, glitch ticks
//   vBytesPerSecond (mempool inflow)      -> hi-hat density

(
// ---------- tunables ----------
~root = 48;   // MIDI root note (48 = C3). Change me.
~bpm = 96;
// false: each transaction sounds the instant it arrives - use this with ompool, whose
//        /btc/tx messages fire exactly when a transaction is revealed on screen.
// true:  transactions are held and played on the 16th-note grid (up to ~156 ms late).
~quantize = false;

s.waitForBoot({
	// ---------- tear down previous evaluation ----------
	~tasks.do(_.stop);
	s.freeAll;
	~padSynth = nil;
	~revBus !? (_.free);
	~delBus !? (_.free);

	// ---------- synth defs ----------
	// melody voice: a soft celesta / music-box tone (sine core + short bright strike)
	SynthDef(\celesta, { |out=0, freq=440, amp=0.4, pan=0, dur=0.25, revBus=0, rev=0.2, delBus=0, del=0.15|
		var env = EnvGen.kr(Env.perc(0.002, dur * 2.5, 1, -5), doneAction: 2);
		var strike = EnvGen.kr(Env.perc(0.001, 0.05));
		var sig = SinOsc.ar(freq * [1, 1.003]).sum * 0.5;
		sig = sig + (SinOsc.ar(freq * 2.0) * strike * 0.3);
		sig = sig + (SinOsc.ar(freq * 3.01) * strike * 0.12);
		sig = Pan2.ar(sig * env * amp, pan);
		Out.ar(out, sig);
		Out.ar(revBus, sig * rev);
		Out.ar(delBus, sig * del);
	}).add;

	// transaction voice: plucked string attack + sine body. Clear transient so each tx reads as an event.
	SynthDef(\ping, { |out=0, freq=440, amp=0.5, pan=0, dec=0.4, bright=0.6, revBus=0, rev=0.2|
		// excitation is lowpassed noise so small/dull transactions pluck softly, not clicky
		var exc = LPF.ar(WhiteNoise.ar, 1200 + (bright * 6000)) * EnvGen.kr(Env.perc(0.002, 0.01));
		var pluck = Pluck.ar(exc, Impulse.kr(0), 0.05, 1 / freq, dec, (1 - bright) * 0.5);
		var body = SinOsc.ar(freq) * EnvGen.kr(Env.perc(0.003, dec, 1, -4)) * 0.5;
		var kill = EnvGen.kr(Env.linen(0, dec + 0.1, 0.05), doneAction: 2);
		var sig = LPF.ar((pluck * 0.8) + body, 2000 + (bright * 7000));
		sig = Pan2.ar(sig * amp * kill, pan);
		Out.ar(out, sig);
		Out.ar(revBus, sig * rev);
	}).add;

	// pad: 5 detuned-saw voices (4 chord tones + an optional 9th) through a warm lowpass
	SynthDef(\pad, { |out=0, revBus=0, gate=1, amp=0.035, cutoff=900, atk=3, rel=5|
		var freqs = \freqs.kr(Array.fill(5, 220));
		var vamps = \vamps.kr(Array.fill(5, 1));
		var env = EnvGen.kr(Env.asr(atk, 1, rel, -2), gate, doneAction: 2);
		var sig = Mix.fill(5, { |i|
			LFSaw.ar(freqs[i] * [0.996, 1, 1.004]).sum * 0.33 * vamps[i]
		});
		sig = RLPF.ar(sig, cutoff.lag(2), 0.5) * env * amp;
		sig = [sig, DelayC.ar(sig, 0.03, 0.012)];
		Out.ar(out, sig);
		Out.ar(revBus, sig * 0.5);
	}).add;

	SynthDef(\bass, { |out=0, freq=55, amp=0.35, cutoff=500, dur=0.5|
		var env = EnvGen.kr(Env.linen(0.01, dur, 0.15, 1, -3), doneAction: 2);
		var sig = (SinOsc.ar(freq) * 0.6) + (LFSaw.ar(freq * [1, 1.005]).sum * 0.2);
		sig = RLPF.ar(sig, cutoff.lag(0.5), 0.4) * env * amp;
		Out.ar(out, sig ! 2);
	}).add;

	SynthDef(\kick, { |out=0, amp=0.8|
		var penv = EnvGen.kr(Env([140, 48, 40], [0.03, 0.25], -4));
		var env = EnvGen.kr(Env.perc(0.001, 0.32, 1, -6), doneAction: 2);
		var sig = SinOsc.ar(penv) * env;
		sig = sig + (WhiteNoise.ar * EnvGen.kr(Env.perc(0.001, 0.01)) * 0.3);
		Out.ar(out, (sig * amp).tanh ! 2);
	}).add;

	SynthDef(\snare, { |out=0, amp=0.5, revBus=0, rev=0.2|
		var env = EnvGen.kr(Env.perc(0.001, 0.12, 1, -5), doneAction: 2);
		var tone = SinOsc.ar(EnvGen.kr(Env([220, 180], [0.05]))) * 0.4;
		var noise = (BPF.ar(WhiteNoise.ar, 1800, 0.8) * 0.7) + (HPF.ar(WhiteNoise.ar, 6000) * 0.15);   // darker, shorter
		var sig = (tone + noise) * env * amp;
		Out.ar(out, sig ! 2);
		Out.ar(revBus, (sig * rev) ! 2);
	}).add;

	SynthDef(\hat, { |out=0, amp=0.25, dec=0.04, pan=0|
		var env = EnvGen.kr(Env.perc(0.001, dec, 1, -6), doneAction: 2);
		var sig = HPF.ar(WhiteNoise.ar, 7500) * env * amp;
		Out.ar(out, Pan2.ar(sig, pan));
	}).add;

	SynthDef(\click, { |out=0, amp=0.3, freq=3000, pan=0, delBus=0|
		var env = EnvGen.kr(Env.perc(0.0005, 0.02, 1, -8), doneAction: 2);
		var sig = Pan2.ar(SinOsc.ar(freq) * env * amp, pan);
		Out.ar(out, sig);
		Out.ar(delBus, sig * 0.5);
	}).add;

	// gong: inharmonic additive partials, long decay, sub thump underneath
	SynthDef(\gong, { |out=0, revBus=0, freq=130, amp=0.5, bright=0.5, decay=9|
		var ratios = [1, 1.51, 2.06, 2.71, 3.42, 4.16, 5.3, 6.62];
		var sig = Mix.fill(8, { |i|
			var d = decay / (1 + (i * 0.6));
			var env = EnvGen.kr(Env.perc(0.004 + (i * 0.003), d, 1, -3));
			SinOsc.ar(freq * ratios[i] * (1 + (LFNoise1.kr(0.3) * 0.002))) * env / (1 + (i * (1.6 - bright)))
		});
		var sub = SinOsc.ar(freq * 0.5) * EnvGen.kr(Env.perc(0.005, 2.5, 1, -4)) * 0.8;
		var kill = EnvGen.kr(Env.linen(0, decay + 1, 0.1), doneAction: 2);
		sig = Pan2.ar(((sig * 0.25) + sub) * amp * kill, 0);
		Out.ar(out, sig);
		Out.ar(revBus, sig * 0.8);
	}).add;

	// fx
	SynthDef(\reverb, { |out=0, in=0, mix=1, room=0.92, damp=0.45, amp=1|
		var sig = In.ar(in, 2);
		Out.ar(out, FreeVerb2.ar(sig[0], sig[1], mix, room, damp) * amp);
	}).add;

	SynthDef(\delay, { |out=0, in=0, time=0.46875, fb=0.45, amp=0.6|
		var sig = In.ar(in, 2);
		var decay = time * (0.001.log / fb.clip(0.01, 0.95).log);
		var del = LPF.ar(CombC.ar(sig, 1.0, time, decay), 3500);
		Out.ar(out, [del[1], del[0]] * amp);   // ping-pong
	}).add;

	SynthDef(\master, { |out=0, amp=0.9|
		var sig = In.ar(out, 2);
		ReplaceOut.ar(out, Limiter.ar(sig * amp, 0.95, 0.01));
	}).add;

	s.sync;

	// ---------- buses, groups, fx chain ----------
	~revBus = Bus.audio(s, 2);
	~delBus = Bus.audio(s, 2);
	~srcGroup = Group.new(s);
	~fxGroup = Group.after(~srcGroup);
	~reverb = Synth(\reverb, [\in, ~revBus], ~fxGroup);
	~delay = Synth(\delay, [\in, ~delBus, \time, 60 / ~bpm * 0.75], ~fxGroup);   // dotted 8th
	~master = Synth.tail(~fxGroup, \master);

	// ---------- musical state ----------
	~clock = ~clock ?? { TempoClock(~bpm / 60).permanent_(true) };
	~clock.tempo = ~bpm / 60;

	// chord progression as semitone offsets from ~root; blocks step through it
	~progression = [
		[0, 4, 7, 11],     // Imaj7
		[9, 12, 16, 19],   // vi7
		[5, 9, 12, 16],    // IVmaj7
		[5, 8, 12, 15],    // iv (borrowed minor) - the melancholy move
		[0, 4, 7, 11],     // Imaj7
		[2, 5, 9, 12],     // ii7
		[7, 11, 14, 17],   // V7
		[4, 7, 11, 14],    // iii7
	];
	~chordIndex = 0;
	~tension = 0.2;    // 0..1  from fastestFee (fee pressure on the network)
	~inflow = 0.3;     // 0..1  from vBytesPerSecond (how fast the mempool is filling)
	~market = 3.0;     // halfHourFee, sat/vB - what "normal" costs right now
	~txQueue = List.new;
	~lastIdx = 4;
	~ratchet = 0;
	~lastRatchet = -10;
	~blockHeight = 0;

	~chord = { ~progression.wrapAt(~chordIndex) };

	// melody pitch pool: chord tones over three octaves from the root (value picks the octave)
	~pool = { |chord|
		var pcs = chord.collect(_ % 12).asSet.asArray.sort;
		var pool = List.new;
		[0, 12, 24].do { |oct| pcs.do { |pc| pool.add(~root + oct + pc) } };
		pool.add(~root + 36 + pcs[0]);
		pool.asArray
	};

	~padFreqs = { |chord|
		var notes = chord.collect { |n| ~root + n };
		var ninth = ~root + chord[0] + 14;
		(notes ++ [ninth]).midicps
	};

	~setPad = { |retrigger=true|
		var freqs = ~padFreqs.(~chord.());
		var ninthAmp = ~tension.linlin(0.4, 0.9, 0, 1);          // 9th fades in as fees rise
		var cutoff = ~tension.linexp(0, 1, 500, 2400);           // calm = warm/dark, congested = bright
		if (retrigger or: ~padSynth.isNil) {
			~padSynth !? { |old| old.set(\gate, 0) };            // old pad releases, new one fades in
			~padSynth = Synth(\pad, [\freqs, freqs, \vamps, [1, 1, 1, 1, ninthAmp], \cutoff, cutoff, \revBus, ~revBus], ~srcGroup);
		} {
			~padSynth.set(\vamps, [1, 1, 1, 1, ninthAmp], \cutoff, cutoff);
		};
	};

	~onBlock = { |height, txCount|
		var bright = txCount.clip(500, 5000).linlin(500, 5000, 0.2, 1.0);
		var hit = {
			Synth(\gong, [\freq, (~root + ~chord.()[0]).midicps, \amp, 0.55, \bright, bright, \revBus, ~revBus], ~srcGroup);
			~setPad.(true);
			nil
		};
		~chordIndex = ~chordIndex + 1;
		~blockHeight = height;
		if (~quantize) { ~clock.play(hit, quant: 0.5) } { hit.value };
		("BLOCK " ++ height ++ "  (" ++ txCount ++ " txs) -> chord " ++ (~chordIndex % ~progression.size)).postln;
	};

	~playTx = { |tx|
		var pool = ~pool.(~chord.());
		var ratio = (tx.feeRate / ~market).clip(0.25, 6);          // how urgent vs. the market
		var size = tx.sats.max(1).log10.clip(3, 9);                // 1k sats .. 10 BTC, six decades
		var idx = ratio.explin(0.25, 6, 0, pool.size - 1).round.asInteger;   // FEE -> pitch: cheap = low, market = middle, urgent = high
		var pan = ratio.explin(0.25, 6, -0.7, 0.7) + 0.1.rand2;   // fee: cheap = left, urgent = right
		var bright = ratio.explin(0.25, 6, 0.2, 0.7);              // fee: cheap = soft/dull, urgent = bright
		var wet = size.linlin(3, 9, 0.25, 0.7);                    // big amounts ring in more space
		var amp = size.linlin(3, 9, 0.28, 0.6) * rrand(0.9, 1.1);
		var dec = size.linlin(3, 9, 0.25, 0.9);
		var note;
		// a run of transactions on the same note (a burst of similar amounts)
		// ornaments: step to a neighbouring chord tone and back
		if (idx == ~lastIdx) {
			~repeats = (~repeats ? 0) + 1;
			if (~repeats.odd) { idx = (idx + [-1, 1].choose).clip(0, pool.size - 1) };
		} {
			~repeats = 0;
		};
		~lastIdx = idx;
		note = pool[idx];
		Synth(\ping, [\freq, note.midicps, \amp, amp, \pan, pan, \dec, dec, \bright, bright, \rev, wet, \revBus, ~revBus], ~srcGroup);
		if (tx.sats >= 1e8) {                                      // whale (>= 1 BTC): deep bell an octave below
			Synth(\ping, [\freq, (note - 12).midicps, \amp, 0.6, \pan, pan * 0.5, \dec, 2.5, \bright, 0.35, \rev, 0.9, \revBus, ~revBus], ~srcGroup);
		};
	};

	// ---------- OSC in ----------
	OSCdef(\btcTx, { |msg|
		var value = msg[1], feeRate = msg[2];
		var tx = (sats: value, feeRate: feeRate);   // (key is 'sats' - 'value' collides with a method name)
		if (~quantize) {
			~txQueue.add(tx);
			while { ~txQueue.size > 48 } { ~txQueue.removeAt(0) };   // only drops if hopelessly behind
		} {
			~playTx.(tx);                                             // sound it now, in sync with the display
		};
		// urgent overpayer -> ratchet: must be > 5x market AND > 5 sat/vB outright (so a
		// near-empty mempool doesn't count 2 sat/vB as urgent), at most once per bar
		if ((feeRate / ~market) > 5 and: { feeRate > 5 } and: { (~clock.beats - ~lastRatchet) > 4 }) {
			~ratchet = 4;
			~lastRatchet = ~clock.beats;
		};
	}, '/btc/tx');

	OSCdef(\btcBlock, { |msg| ~onBlock.(msg[1].asInteger, msg[2].asInteger) }, '/btc/block');

	OSCdef(\btcFees, { |msg|
		var fastest = msg[1], halfHour = msg[2];
		~market = halfHour.max(1);
		~tension = fastest.clip(1, 150).explin(1, 150, 0, 1);
		~setPad.(false);
	}, '/btc/fees');

	OSCdef(\btcMempool, { |msg|
		~inflow = msg[3].clip(100, 6000).explin(100, 6000, 0, 1);
	}, '/btc/mempool');

	// ompool's Audio Export view sends one of these per grid step: [bpm, step, stepsPerBar].
	// Adopt its tempo, and on each bar start lock our sequencer to its bar so the
	// drums land with the transactions it reveals.
	OSCdef(\btcClock, { |msg|
		var bpm = msg[1], step = msg[2].asInteger;
		var beats, nearest, offset;
		if ((bpm / 60 - ~clock.tempo).abs > 0.001) {   // ompool's tempo glides with network busyness
			if ((bpm - ~bpm).abs >= 2) { ("clock: " ++ bpm.round(1) ++ " bpm from ompool").postln; ~bpm = bpm };
			~clock.tempo = bpm / 60;
			~delay.set(\time, 60 / bpm * 0.75);
		};
		if (step == 0) {
			beats = ~clock.beats;
			nearest = (beats / 4).round * 4;
			offset = beats - nearest;
			if (offset.abs > 0.25) {                 // far off (first lock): restart the parts on this bar
				~tasks.do { |t| t.stop; t.reset };
				~clock.beats = nearest;
				~tasks.do { |t| t.play(~clock, quant: 4) };
				"clock: locked to ompool bar".postln;
			} {
				if (offset.abs > 0.02) { ~clock.beats = nearest };   // small drift: nudge
			};
		};
	}, '/btc/clock');

	// ---------- sequencers (16th-note grid) ----------
	// every transaction plays: one per 16th; if they're piling up, a second one on the 32nd
	~melodyTask = Task {
		loop {
			if (~txQueue.notEmpty) { ~playTx.(~txQueue.removeAt(0)) };
			if (~txQueue.size > 4) {
				~clock.sched(0.125, { ~txQueue.notEmpty.if { ~playTx.(~txQueue.removeAt(0)) }; nil });
			};
			0.25.wait;
		}
	};

	~kickTmpl  = [1,0,0,0, 0,0,1,0, 0,0,1,0, 0,0,0,0];
	~snareTmpl = [0,0,0,0, 1,0,0,0, 0,0,0,0, 1,0,0,0];

	~drumTask = Task {
		var step = 0;
		loop {
			var s16 = step % 16;
			var hatProb = (~tension * 0.5) + (~inflow * 0.5);
			// drums sit well under the transactions
			if (~kickTmpl[s16] == 1 or: { s16 == 11 and: { ~tension > 0.6 } }) {
				Synth(\kick, [\amp, 0.5], ~srcGroup);
			};
			if (~snareTmpl[s16] == 1) { Synth(\snare, [\amp, 0.14, \revBus, ~revBus], ~srcGroup) };
			if (s16 == 15 and: { (0.25 + (~tension * 0.5)).coin }) {                       // ghost note
				Synth(\snare, [\amp, 0.05, \revBus, ~revBus], ~srcGroup);
			};
			if (s16.even) {
				Synth(\hat, [\amp, if (s16 % 4 == 0) { 0.14 } { 0.08 }, \dec, 0.035, \pan, -0.2], ~srcGroup);
			} {
				if (hatProb.coin) {
					Synth(\hat, [\amp, 0.06, \dec, 0.02 + (0.05 * ~tension), \pan, 0.25], ~srcGroup);
				};
			};
			if (hatProb > 0.6 and: { 0.15.coin }) {                                          // glitch ticks
				Synth(\click, [\freq, exprand(1500, 6000), \amp, 0.2, \pan, 1.0.rand2, \delBus, ~delBus], ~srcGroup);
			};
			if (~ratchet > 0) {                                                                // 4 quiet snare hits at 32nds
				~ratchet = 0;
				4.do { |i|
					~clock.sched(i * 0.125, { Synth(\snare, [\amp, 0.08 + (i * 0.025), \revBus, ~revBus], ~srcGroup); nil });
				};
			};
			step = step + 1;
			0.25.wait;
		}
	};

	~bassTmpl = [1,0,0,0, 0,0,2,0, 1,0,0,0, 0,0,2,1];   // 1 = root, 2 = fifth

	~bassTask = Task {
		var step = 0;
		loop {
			var v = ~bassTmpl.wrapAt(step);
			var rootNote = ~root - 12 + (~chord.()[0] % 12);
			if (v > 0) {
				var note = if (v == 2) { rootNote + 7 } { rootNote };
				Synth(\bass, [\freq, note.midicps, \amp, 0.22, \dur, if (v == 1) { 0.6 } { 0.3 },
					\cutoff, ~tension.linexp(0, 1, 350, 1200)], ~srcGroup);
			};
			step = step + 1;
			0.25.wait;
		}
	};

	// ---------- go ----------
	~tasks = [~melodyTask, ~drumTask, ~bassTask];
	~setPad.(true);
	~tasks.do { |t| t.play(~clock, quant: 4) };

	("instrument ready: " ++ ~bpm ++ " bpm, root " ++ ~root ++ ", listening on port " ++ NetAddr.langPort).postln;
});
)
