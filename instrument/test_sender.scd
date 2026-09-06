// Fakes the live feed so you can audition instrument.scd without waiting on the
// network (or ~10 minutes for a real block). Run the instrument in SCIDE first
// (see the header of ompool.scd, or `ompool instrument --test-sender > test.scd`), then this.
//
// Sends: fee/mempool stats -> a calm stream of tx -> a BLOCK (gong + chord change)
//        -> a congested, high-fee stream with an urgent overpayer (ratchet) and a whale.

(
fork {
	var addr = NetAddr("127.0.0.1", NetAddr.langPort);

	// calm network: fastest 3 sat/vB, market 2, low inflow
	addr.sendMsg('/btc/fees', 3.0, 2.0, 1.5, 1.0, 1.0);
	addr.sendMsg('/btc/mempool', 8000, 4000000, 400);
	"-- calm --".postln;
	16.do {
		addr.sendMsg('/btc/tx', exprand(20000, 5000000), exprand(1.0, 4.0));
		rrand(0.12, 0.35).wait;
	};
	2.wait;

	// a block lands: gong + chord change
	"-- BLOCK --".postln;
	addr.sendMsg('/btc/block', 900001, 3200);
	4.wait;

	// congestion: fees spike, inflow high -> busier template, brighter pad, 9ths, more hats
	"-- congested --".postln;
	addr.sendMsg('/btc/fees', 60.0, 40.0, 25.0, 10.0, 5.0);
	addr.sendMsg('/btc/mempool', 120000, 90000000, 4500);
	20.do { |i|
		var rate = exprand(20.0, 80.0);
		if (i == 8) { rate = 200.0; "   urgent overpayer -> ratchet".postln };
		addr.sendMsg('/btc/tx', exprand(50000, 20000000), rate);
		rrand(0.08, 0.25).wait;
	};
	"   whale: 2.5 BTC".postln;
	addr.sendMsg('/btc/tx', 250000000, 45.0);
	4.wait;
	"-- test done (sequencer keeps running; Ctrl+. to stop) --".postln;
};
)

// handy one-liners:
// NetAddr("127.0.0.1", NetAddr.langPort).sendMsg('/btc/block', 900002, 2800);   // force a block
// ~tension = 0.9; ~setPad.(false);                                              // force high tension
