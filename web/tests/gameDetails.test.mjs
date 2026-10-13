import assert from 'node:assert/strict';
import { test } from 'node:test';
import { answerFor } from '../src/lib/gameDetails.ts';

test("a sheet shows only its own game's details, never the previous game's while the next one loads", () => {
  const answer = { gameId: 'a', details: { genres: ['Puzzle'] }, warnings: ['slow'], error: '' };
  assert.equal(answerFor(answer, 'a'), answer);
  assert.equal(answerFor(answer, 'b'), null, 'moved to another game: nothing until its answer arrives');
  assert.equal(answerFor(null, 'a'), null, 'nothing asked yet');
});
