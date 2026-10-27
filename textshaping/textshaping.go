// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package textshaping ;import (_d "github.com/OpenMellon/openpdf/garabic";_f "golang.org/x/text/unicode/bidi";_c "strings";);

// ArabicShape returns shaped arabic glyphs string.
func ArabicShape (text string )(string ,error ){_ff :=_f .Paragraph {};_ff .SetString (text );_fa ,_db :=_ff .Order ();if _db !=nil {return "",_db ;};for _da :=0;_da < _fa .NumRuns ();_da ++{_dc :=_fa .Run (_da );_g :=_dc .String ();if _dc .Direction ()==_f .RightToLeft {var (_e =_d .Shape (_g );
_a =[]rune (_e );_fd =make ([]rune ,len (_a )););_ge :=0;for _bg :=len (_a )-1;_bg >=0;_bg --{_fd [_ge ]=_a [_bg ];_ge ++;};_g =string (_fd );text =_c .Replace (text ,_c .TrimSpace (_dc .String ()),_g ,1);};};return text ,nil ;};